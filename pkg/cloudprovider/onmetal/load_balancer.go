package onmetal

import (
	"context"

	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalLoadBalancer struct {
	clientSet *onmetalapi.Clientset
}

func newOnmetalLoadBalancer(clientSet *onmetalapi.Clientset) cloudprovider.LoadBalancer {
	return &onmetalLoadBalancer{
		clientSet: clientSet,
	}
}

func (o *onmetalLoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	return nil, false, cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	return ""
}

func (o *onmetalLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	return cloudprovider.NotImplemented
}
