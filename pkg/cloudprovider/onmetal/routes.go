package onmetal

import (
	"context"

	cloudprovider "k8s.io/cloud-provider"
)

type onmetalRoutes struct {
}

func newOnmetalRoutes() cloudprovider.Routes {
	return &onmetalRoutes{}
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
