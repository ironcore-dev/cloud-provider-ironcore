package onmetal

import (
	"context"

	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalZones struct {
	clientSet *onmetalapi.Clientset
}

func newOnmetalZones(clientSet *onmetalapi.Clientset) cloudprovider.Zones {
	return &onmetalZones{
		clientSet: clientSet,
	}
}

func (o *onmetalZones) GetZone(ctx context.Context) (cloudprovider.Zone, error) {
	return cloudprovider.Zone{}, cloudprovider.NotImplemented
}

func (o *onmetalZones) GetZoneByProviderID(ctx context.Context, providerID string) (cloudprovider.Zone, error) {
	return cloudprovider.Zone{}, cloudprovider.NotImplemented
}

func (o *onmetalZones) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	return cloudprovider.Zone{}, cloudprovider.NotImplemented
}
