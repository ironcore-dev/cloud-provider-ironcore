package onmetal

import (
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalInstances struct {
}

func newOnmetalInstances() cloudprovider.Instances {
	return &onmetalInstances{}
}

func (o *onmetalInstances) NodeAddresses(ctx context.Context, name types.NodeName) ([]v1.NodeAddress, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalInstances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]v1.NodeAddress, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalInstances) InstanceID(_ context.Context, nodeName types.NodeName) (string, error) {
	return "", cloudprovider.NotImplemented
}

func (o *onmetalInstances) InstanceType(ctx context.Context, nodeName types.NodeName) (string, error) {
	return "", cloudprovider.NotImplemented
}

func (o *onmetalInstances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	return "", cloudprovider.NotImplemented
}

func (o *onmetalInstances) AddSSHKeyToAllInstances(_ context.Context, _ string, _ []byte) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalInstances) CurrentNodeName(_ context.Context, hostName string) (types.NodeName, error) {
	return "", cloudprovider.NotImplemented
}

func (o *onmetalInstances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	return false, cloudprovider.NotImplemented
}

func (o *onmetalInstances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	return false, cloudprovider.NotImplemented
}

func nodeNameFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, CDefaultCloudProviderName+"://")
}
