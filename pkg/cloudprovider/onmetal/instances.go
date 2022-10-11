// Copyright 2022 OnMetal authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package onmetal

import (
	"context"
	"fmt"
	"strings"

	"github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalInstances struct {
	onmetalClient client.Client
	targetClient  client.Client
	namespace     string
}

func newOnmetalInstances(onmetalClient client.Client, targetClient client.Client, namespace string) cloudprovider.Instances {
	return &onmetalInstances{
		onmetalClient: onmetalClient,
		targetClient:  targetClient,
		namespace:     namespace,
	}
}

func (o *onmetalInstances) NodeAddresses(ctx context.Context, nodeName types.NodeName) ([]corev1.NodeAddress, error) {
	machine, err := GetMachineForNodeName(ctx, o.onmetalClient, o.targetClient, nodeName)
	if apierrors.IsNotFound(err) {
		return nil, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to get machine for node name %s: %w", nodeName, err)
	}
	if machine == nil {
		return nil, nil
	}
	addresses := make([]corev1.NodeAddress, 0)
	for _, iface := range machine.Status.NetworkInterfaces {
		if iface.VirtualIP != nil {
			addresses = append(addresses, corev1.NodeAddress{
				Type:    corev1.NodeExternalIP,
				Address: iface.VirtualIP.String(),
			})
		}
		for _, ip := range iface.IPs {
			ip := ip.String()
			addresses = append(addresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: ip,
			})
		}
	}
	addresses = append(addresses, corev1.NodeAddress{
		Type:    corev1.NodeHostName,
		Address: machine.Name,
	})
	return addresses, nil
}

func (o *onmetalInstances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]corev1.NodeAddress, error) {
	machine, err := GetMachineForProviderID(ctx, o.onmetalClient, providerID)
	if err != nil {
		return nil, err
	}
	return o.NodeAddresses(ctx, types.NodeName(machine.Name))
}

func (o *onmetalInstances) InstanceID(ctx context.Context, nodeName types.NodeName) (string, error) {
	machine, err := GetMachineForNodeName(ctx, o.onmetalClient, o.targetClient, nodeName)
	if apierrors.IsNotFound(err) {
		return "", cloudprovider.InstanceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get machine for node name %s: %w", nodeName, err)
	}
	return string(machine.UID), nil
}

func (o *onmetalInstances) InstanceType(ctx context.Context, nodeName types.NodeName) (string, error) {
	machine, err := GetMachineForNodeName(ctx, o.onmetalClient, o.targetClient, nodeName)
	if apierrors.IsNotFound(err) {
		return "", cloudprovider.InstanceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get machine for node name %s: %w", nodeName, err)
	}
	return machine.Spec.MachineClassRef.Name, nil
}

func (o *onmetalInstances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	return o.InstanceType(ctx, types.NodeName(getMachineUIDFromProviderID(providerID)))
}

func (o *onmetalInstances) AddSSHKeyToAllInstances(_ context.Context, _ string, _ []byte) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalInstances) CurrentNodeName(_ context.Context, hostName string) (types.NodeName, error) {
	return types.NodeName(hostName), nil
}

func (o *onmetalInstances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	machine, err := GetMachineForProviderID(ctx, o.onmetalClient, providerID)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "unable to get machine")
	}
	if machine == nil {
		return false, nil
	}
	return true, nil
}

func (o *onmetalInstances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	machine, err := GetMachineForProviderID(ctx, o.onmetalClient, providerID)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return machine.Status.State == v1alpha1.MachineStateShutdown, nil
}

func getMachineUIDFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, CloudProviderName+"://")
}
