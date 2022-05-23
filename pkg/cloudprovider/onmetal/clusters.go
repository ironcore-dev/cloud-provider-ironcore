package onmetal

import (
	"context"

	cloudprovider "k8s.io/cloud-provider"
)

type onmetalClusters struct {
}

func newOnmetalClusters() cloudprovider.Clusters {
	return &onmetalClusters{}
}

func (o *onmetalClusters) ListClusters(ctx context.Context) ([]string, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalClusters) Master(ctx context.Context, clusterName string) (string, error) {
	return "", cloudprovider.NotImplemented
}
