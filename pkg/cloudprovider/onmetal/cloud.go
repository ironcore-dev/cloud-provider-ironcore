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
	"io"
	"log"
	"time"

	computev1alpha1 "github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	ipamv1alpha1 "github.com/onmetal/onmetal-api/apis/ipam/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/apis/networking/v1alpha1"
	storagev1alpha1 "github.com/onmetal/onmetal-api/apis/storage/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	cloudprovider "k8s.io/cloud-provider"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalCloudProvider struct {
	loadBalancer cloudprovider.LoadBalancer
	instances    cloudprovider.Instances
	instancesV2  cloudprovider.InstancesV2
	clusters     cloudprovider.Clusters
	routes       cloudprovider.Routes
	zones        cloudprovider.Zones
}

func InitCloudProvider(config io.Reader) (cloudprovider.Interface, error) {
	cfg, err := NewConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "can't decode yaml config")
	}
	return newCloudProvider(cfg)
}

func newCloudProvider(config *onmetalCloudProviderConfig) (cloudprovider.Interface, error) {

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
	if err := clusterapiv1beta1.AddToScheme(scheme.Scheme); err != nil {
		return nil, errors.Wrap(err, "unable to add cluster-api api to scheme")
	}

	k8sClient, err := client.New(config.restConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, errors.Wrap(err, "unable to create Kubernetes client")
	}

	loadBalancer := newOnmetalLoadBalancer(k8sClient)
	instances := newOnmetalInstances(k8sClient, config.namespace)
	instancesV2 := newOnmetalInstancesV2(k8sClient, config.namespace)
	clusters := newOnmetalClusters(k8sClient, config.namespace)
	routes := newOnmetalRoutes(k8sClient)
	zones := newOnmetalZones(k8sClient)

	return &onmetalCloudProvider{
		loadBalancer: loadBalancer,
		instances:    instances,
		instancesV2:  instancesV2,
		clusters:     clusters,
		routes:       routes,
		zones:        zones,
	}, nil
}

func (o *onmetalCloudProvider) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {

	clientset := clientBuilder.ClientOrDie("cloud-provider-onmetal")

	informerFactory := informers.NewSharedInformerFactory(clientset, time.Second*30)
	serviceInformer := informerFactory.Core().V1().Services()
	nodeInformer := informerFactory.Core().V1().Nodes()

	go serviceInformer.Informer().Run(stop)
	go nodeInformer.Informer().Run(stop)

	if !cache.WaitForCacheSync(stop, serviceInformer.Informer().HasSynced) {
		log.Fatal("Timed out waiting for caches to sync")
	}
	if !cache.WaitForCacheSync(stop, nodeInformer.Informer().HasSynced) {
		log.Fatal("Timed out waiting for caches to sync")
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
	return o.clusters, true
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
