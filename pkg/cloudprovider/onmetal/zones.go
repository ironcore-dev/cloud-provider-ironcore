package onmetal

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalZones struct {
}

func newOnmetalZones() cloudprovider.Zones {
	return &onmetalZones{}
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
