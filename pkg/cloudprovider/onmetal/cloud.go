package onmetal

import (
	"io"

	"github.com/pkg/errors"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalCloudProvider struct {
}

func InitCloudProvider(config io.Reader) (cloudprovider.Interface, error) {
	cfg, err := NewConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "can't decode yaml config")
	}
	return newCloudProvider(cfg)
}

func newCloudProvider(config *onmetalCloudProviderConfig) (cloudprovider.Interface, error) {
	return &onmetalCloudProvider{}, nil
}

func (o *onmetalCloudProvider) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

func (o *onmetalCloudProvider) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return nil, false
}

func (o *onmetalCloudProvider) Instances() (cloudprovider.Instances, bool) {
	return nil, false
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
