package onmetal

import (
	"context"

	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalInstancesV2 struct {
}

func newOnmetalInstancesV2() cloudprovider.InstancesV2 {
	return &onmetalInstancesV2{}
}

func (o *onmetalInstancesV2) InstanceExists(ctx context.Context, node *v1.Node) (bool, error) {
	return false, cloudprovider.NotImplemented
}

func (o *onmetalInstancesV2) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	return false, cloudprovider.NotImplemented
}

func (o *onmetalInstancesV2) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	return nil, cloudprovider.NotImplemented
}
