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

	computev1alpha1 "github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	ipamv1alpha1 "github.com/onmetal/onmetal-api/apis/ipam/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/apis/networking/v1alpha1"
	storagev1alpha1 "github.com/onmetal/onmetal-api/apis/storage/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

const (
	CloudProviderName       = "onmetal"
	machineMetadataUIDField = ".metadata.uid"
)

type onmetalCloudProvider struct {
	targetCluster  cluster.Cluster
	onmetalCluster cluster.Cluster
	loadBalancer   cloudprovider.LoadBalancer
	instances      cloudprovider.Instances
	instancesV2    cloudprovider.InstancesV2
	routes         cloudprovider.Routes
	zones          cloudprovider.Zones
}

func InitCloudProvider(config io.Reader) (cloudprovider.Interface, error) {
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

	onmetalCluster, err := cluster.New(cfg.onmetalRestConfig, func(o *cluster.Options) {
		o.Scheme = scheme.Scheme
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create onmetal cluster: %w", err)
	}
	onmetalClient := onmetalCluster.GetClient()

	targetCluster, err := cluster.New(cfg.targetRestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create target cluster: %w", err)
	}
	targetClient := targetCluster.GetClient()

	loadBalancer := newOnmetalLoadBalancer(onmetalClient)
	instances := newOnmetalInstances(onmetalClient, targetClient, cfg.namespace)
	instancesV2 := newOnmetalInstancesV2(onmetalClient, cfg.namespace)
	routes := newOnmetalRoutes(onmetalClient)
	zones := newOnmetalZones(onmetalClient)

	return &onmetalCloudProvider{
		onmetalCluster: onmetalCluster,
		targetCluster:  targetCluster,
		loadBalancer:   loadBalancer,
		instances:      instances,
		instancesV2:    instancesV2,
		routes:         routes,
		zones:          zones,
	}, nil
}

func (o *onmetalCloudProvider) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		<-stop
	}()

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
}

func (o *onmetalCloudProvider) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return nil, false
}

func (o *onmetalCloudProvider) Instances() (cloudprovider.Instances, bool) {
	return o.instances, true
}

func (o *onmetalCloudProvider) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return nil, false
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
