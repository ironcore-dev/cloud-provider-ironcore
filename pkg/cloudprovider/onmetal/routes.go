package onmetal

import (
	"context"

	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalRoutes struct {
	clientSet *onmetalapi.Clientset
}

func newOnmetalRoutes(clientSet *onmetalapi.Clientset) cloudprovider.Routes {
	return &onmetalRoutes{
		clientSet: clientSet,
	}
}

func (o *onmetalRoutes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalRoutes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalRoutes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	return cloudprovider.NotImplemented
}
