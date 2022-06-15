package onmetal

import (
	"io"
	"log"
	"time"

	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	"github.com/pkg/errors"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	cloudprovider "k8s.io/cloud-provider"
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
	onmetalClientSet, err := onmetalapi.NewForConfig(config.restConfig)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get onmetal clientset")
	}
	loadBalancer := newOnmetalLoadBalancer(onmetalClientSet)
	instances := newOnmetalInstances(onmetalClientSet, config.namespace)
	instancesV2 := newOnmetalInstancesV2(onmetalClientSet, config.namespace)
	clusters := newOnmetalClusters(onmetalClientSet)
	routes := newOnmetalRoutes(onmetalClientSet)
	zones := newOnmetalZones(onmetalClientSet)
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
	return nil, false
}

func (o *onmetalCloudProvider) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (o *onmetalCloudProvider) ProviderName() string {
	return CDefaultCloudProviderName
}

func (o *onmetalCloudProvider) HasClusterID() bool {
	return true
}
