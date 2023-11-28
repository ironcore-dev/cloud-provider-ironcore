// Copyright 2022 OnMetal authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ironcore

import (
	"context"
	"fmt"
	"io"
	"log"

	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
	storagev1alpha1 "github.com/ironcore-dev/ironcore/api/storage/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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

	o.instancesV2 = newIroncoreInstancesV2(o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace, o.cloudConfig.ClusterName)
	o.loadBalancer = newIroncoreLoadBalancer(o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace, o.cloudConfig)
	o.routes = newIroncoreRoutes(o.targetCluster.GetClient(), o.ironcoreCluster.GetClient(), o.ironcoreNamespace, o.cloudConfig)

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
