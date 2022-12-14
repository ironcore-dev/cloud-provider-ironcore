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

package onmetal

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	ipamv1alpha1 "github.com/onmetal/onmetal-api/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	storagev1alpha1 "github.com/onmetal/onmetal-api/api/storage/v1alpha1"
)

const (
	CloudProviderName       = "onmetal"
	machineMetadataUIDField = ".metadata.uid"
)

type onmetalCloudProvider struct {
	targetCluster    cluster.Cluster
	onmetalCluster   cluster.Cluster
	onmetalNamespace string
	networkName      string
	loadBalancer     cloudprovider.LoadBalancer
	instancesV2      cloudprovider.InstancesV2
}

func InitCloudProvider(config io.Reader) (cloudprovider.Interface, error) {
	klog.V(2).Infof("Setting up cloud provider: %s", CloudProviderName)

	cfg, err := NewConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "can't decode yaml config")
	}

	if err := computev1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, errors.Wrap(err, "unable to add onmetal compute api to scheme")
	}
	if err := storagev1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, errors.Wrap(err, "unable to add onmetal storage api to scheme")
	}
	if err := ipamv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, errors.Wrap(err, "unable to add onmetal ipam api to scheme")
	}
	if err := networkingv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, errors.Wrap(err, "unable to add onmetal networking api to scheme")
	}

	onmetalCluster, err := cluster.New(cfg.RestConfig, func(o *cluster.Options) {
		o.Scheme = scheme.Scheme
		o.Namespace = cfg.Namespace
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create onmetal cluster: %w", err)
	}

	klog.V(2).Infof("Successfully setup cloud provider: %s", CloudProviderName)
	return &onmetalCloudProvider{
		onmetalCluster:   onmetalCluster,
		onmetalNamespace: cfg.Namespace,
		networkName:      cfg.NetworkName,
	}, nil
}

func (o *onmetalCloudProvider) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	klog.V(2).Infof("Initializing cloud provider: %s", CloudProviderName)
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

	o.instancesV2 = newOnmetalInstancesV2(o.targetCluster.GetClient(), o.onmetalCluster.GetClient(), o.onmetalNamespace)
	o.loadBalancer = newOnmetalLoadBalancer(o.targetCluster.GetClient(), o.onmetalCluster.GetClient(), o.onmetalNamespace, o.networkName)

	if err := o.onmetalCluster.GetFieldIndexer().IndexField(ctx, &computev1alpha1.Machine{}, machineMetadataUIDField, func(object client.Object) []string {
		machine := object.(*computev1alpha1.Machine)
		return []string{string(machine.UID)}
	}); err != nil {
		log.Fatalf("Failed to setup field indexer: %v", err)
	}

	if _, err := o.targetCluster.GetCache().GetInformer(ctx, &corev1.Node{}); err != nil {
		log.Fatalf("Failed to setup Node informer: %v", err)
	}
	// TODO: setup informer for Services

	go func() {
		if err := o.onmetalCluster.Start(ctx); err != nil {
			log.Fatalf("Failed to start onmetal cluster: %v", err)
		}
	}()

	go func() {
		if err := o.targetCluster.Start(ctx); err != nil {
			log.Fatalf("Failed to start target cluster: %v", err)
		}
	}()

	if !o.onmetalCluster.GetCache().WaitForCacheSync(ctx) {
		log.Fatal("Failed to wait for onmetal cluster cache to sync")
	}
	if !o.targetCluster.GetCache().WaitForCacheSync(ctx) {
		log.Fatal("Failed to wait for target cluster cache to sync")
	}
	klog.V(2).Infof("Successfully initialized cloud provider: %s", CloudProviderName)
}

func (o *onmetalCloudProvider) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return o.loadBalancer, true
}

func (o *onmetalCloudProvider) Instances() (cloudprovider.Instances, bool) {
	return nil, true
}

func (o *onmetalCloudProvider) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return o.instancesV2, true
}

func (o *onmetalCloudProvider) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

func (o *onmetalCloudProvider) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

func (o *onmetalCloudProvider) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (o *onmetalCloudProvider) ProviderName() string {
	return CloudProviderName
}

func (o *onmetalCloudProvider) HasClusterID() bool {
	return true
}
