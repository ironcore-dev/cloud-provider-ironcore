package onmetal

import (
	"context"

	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalClusters struct {
	clientSet *onmetalapi.Clientset
}

func newOnmetalClusters(clientSet *onmetalapi.Clientset) cloudprovider.Clusters {
	return &onmetalClusters{
		clientSet: clientSet,
	}
}

func (o *onmetalClusters) ListClusters(ctx context.Context) ([]string, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalClusters) Master(ctx context.Context, clusterName string) (string, error) {
	return "", cloudprovider.NotImplemented
}
