// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"k8s.io/utils/ptr"

	nodeutil "github.com/gardener/aws-ipam-controller/pkg/node"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
	storagev1alpha1 "github.com/ironcore-dev/ironcore/api/storage/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	gocache "k8s.io/client-go/tools/cache"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

const (
	ProviderName                            = "ironcore"
	machineMetadataUIDField                 = ".metadata.uid"
	networkInterfaceSpecNetworkRefNameField = "spec.networkRef.name"
)

var ironcoreScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(computev1alpha1.AddToScheme(ironcoreScheme))
	utilruntime.Must(storagev1alpha1.AddToScheme(ironcoreScheme))
	utilruntime.Must(ipamv1alpha1.AddToScheme(ironcoreScheme))
	utilruntime.Must(networkingv1alpha1.AddToScheme(ironcoreScheme))

	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cfg, err := LoadCloudProviderConfig(config)
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode config")
		}

		ironcoreCluster, err := cluster.New(cfg.RestConfig, func(o *cluster.Options) {
			o.Scheme = ironcoreScheme
			o.Cache.DefaultNamespaces = map[string]cache.Config{
				cfg.Namespace: {},
			}
		})
		if err != nil {
			return nil, fmt.Errorf("unable to create ironcore cluster: %w", err)
		}

		return &cloud{
			ironcoreCluster:   ironcoreCluster,
			ironcoreNamespace: cfg.Namespace,
			cloudConfig:       cfg.cloudConfig,
		}, nil
	})
}

type cloud struct {
	targetCluster     cluster.Cluster
	ironcoreCluster   cluster.Cluster
	ironcoreNamespace string
	cloudConfig       CloudConfig
	loadBalancer      cloudprovider.LoadBalancer
	instancesV2       cloudprovider.InstancesV2
	routes            cloudprovider.Routes
	cidrAllocator     CIDRAllocator
}

func (o *cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	klog.V(2).Infof("Initializing cloud provider: %s", ProviderName)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		<-stop
	}()

	cfg, err := clientBuilder.Config("cloud-controller-manager")
	if err != nil {
		log.Fatalf("Failed to get config: %v", err)
	}
	o.targetCluster, err = cluster.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create new cluster: %v", err)
	}

	nodeInformer, err := o.targetCluster.GetCache().GetInformer(ctx, &corev1.Node{})
	if err != nil {
		klog.Fatalf("Failed to get informer for Node: %v", err)
	}
	_, cidr, err := net.ParseCIDR("100.80.0.0/12")
	if err != nil {
		klog.Fatalf("Failed to parse CIDR: %v", err)
	}
	o.instancesV2 = newIroncoreInstancesV2(o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace, o.cloudConfig.ClusterName)
	o.loadBalancer = newIroncoreLoadBalancer(o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace, o.cloudConfig)
	o.routes = newIroncoreRoutes(o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace, o.cloudConfig)
	o.cidrAllocator, err = NewCIDRRangeAllocator(ctx, o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace,
		CIDRAllocatorParams{
			ClusterCIDRs:      []*net.IPNet{cidr},
			NodeCIDRMaskSizes: []int{24},
		}, nodeInformer, "dual-stack", ptr.To(500*time.Millisecond), 112)
	if err != nil {
		klog.Fatalf("Failed to initialize CIDR allocator: %v", err)
	}

	if err := o.ironcoreCluster.GetFieldIndexer().IndexField(ctx, &computev1alpha1.Machine{}, machineMetadataUIDField, func(object client.Object) []string {
		machine := object.(*computev1alpha1.Machine)
		return []string{string(machine.UID)}
	}); err != nil {
		log.Fatalf("Failed to setup field indexer for machine: %v", err)
	}

	if err := o.ironcoreCluster.GetFieldIndexer().IndexField(ctx, &networkingv1alpha1.NetworkInterface{}, networkInterfaceSpecNetworkRefNameField, func(object client.Object) []string {
		nic := object.(*networkingv1alpha1.NetworkInterface)
		return []string{nic.Spec.NetworkRef.Name}
	}); err != nil {
		log.Fatalf("Failed to setup field indexer for network interface: %v", err)
	}

	if _, err := o.targetCluster.GetCache().GetInformer(ctx, &corev1.Node{}); err != nil {
		log.Fatalf("Failed to setup Node informer: %v", err)
	}
	// TODO: setup informer for Services

	go func() {
		if err := o.ironcoreCluster.Start(ctx); err != nil {
			log.Fatalf("Failed to start ironcore cluster: %v", err)
		}
	}()

	go func() {
		if err := o.targetCluster.Start(ctx); err != nil {
			log.Fatalf("Failed to start target cluster: %v", err)
		}
	}()

	if !o.ironcoreCluster.GetCache().WaitForCacheSync(ctx) {
		log.Fatal("Failed to wait for ironcore cluster cache to sync")
	}
	if !o.targetCluster.GetCache().WaitForCacheSync(ctx) {
		log.Fatal("Failed to wait for target cluster cache to sync")
	}
	_, err = nodeInformer.AddEventHandler(gocache.ResourceEventHandlerFuncs{
		AddFunc: nodeutil.CreateAddNodeHandler(o.cidrAllocator.AllocateOrOccupyCIDR),
		UpdateFunc: nodeutil.CreateUpdateNodeHandler(func(_, newNode *corev1.Node) error {
			// If the PodCIDRs list is not empty we either:
			// - already processed a Node that already had CIDRs after NC restarted
			//   (cidr is marked as used),
			// - already processed a Node successfully and allocated CIDRs for it
			//   (cidr is marked as used),
			// - already processed a Node but we did saw a "timeout" response and
			//   request eventually got through in this case we haven't released
			//   the allocated CIDRs (cidr is still marked as used).
			// There's a possible error here:
			// - NC sees a new Node and assigns CIDRs X,Y.. to it,
			// - Update Node call fails with a timeout,
			// - Node is updated by some other component, NC sees an update and
			//   assigns CIDRs A,B.. to the Node,
			// - Both CIDR X,Y.. and CIDR A,B.. are marked as used in the local cache,
			//   even though Node sees only CIDR A,B..
			// The problem here is that in in-memory cache we see CIDR X,Y.. as marked,
			// which prevents it from being assigned to any new node. The cluster
			// state is correct.
			// Restart of NC fixes the issue.
			if len(newNode.Spec.PodCIDRs) == 0 {
				return o.cidrAllocator.AllocateOrOccupyCIDR(newNode)
			}
			return nil
		}),
		DeleteFunc: nodeutil.CreateDeleteNodeHandler(o.cidrAllocator.ReleaseCIDR),
	})
	if err != nil {
		klog.Error(err, " unable to add components informer event handler")
		os.Exit(1)
	}

	// Create the stopCh channel
	stopCh := make(chan struct{})
	go o.cidrAllocator.Run(ctx, stopCh)

	klog.V(2).Infof("Successfully initialized cloud provider: %s", ProviderName)
}

func (o *cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return o.loadBalancer, true
}

// Instances returns an implementation of Instances for ironcore
func (o *cloud) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

// InstancesV2 is an implementation for instances and should only be implemented by external cloud providers.
// Implementing InstancesV2 is behaviorally identical to Instances but is optimized to significantly reduce
// API calls to the cloud provider when registering and syncing nodes.
// Also returns true if the interface is supported, false otherwise.
func (o *cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return o.instancesV2, true
}

// Zones returns an implementation of Zones for ironcore
func (o *cloud) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

// Clusters returns the list of clusters
func (o *cloud) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

// Routes returns an implementation of Routes for ironcore
func (o *cloud) Routes() (cloudprovider.Routes, bool) {
	return o.routes, true
}

// ProviderName returns the cloud provider ID
func (o *cloud) ProviderName() string {
	return ProviderName
}

// HasClusterID returns true if the cluster has a clusterID
func (o *cloud) HasClusterID() bool {
	return true
}
