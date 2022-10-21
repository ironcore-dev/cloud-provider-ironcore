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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalInstances struct {
	targetClient  client.Client
	onmetalClient client.Client
	namespace     string
}

func newOnmetalInstances(targetClient client.Client, onmetalClient client.Client, namespace string) cloudprovider.Instances {
	return &onmetalInstances{
		targetClient:  targetClient,
		onmetalClient: onmetalClient,
		namespace:     namespace,
	}
}

func (o *onmetalInstances) NodeAddresses(ctx context.Context, nodeName types.NodeName) ([]corev1.NodeAddress, error) {
	klog.V(4).Infof("Getting node addresses for node: %s", nodeName)
	machine, err := GetMachineForNodeName(ctx, o.onmetalClient, o.namespace, nodeName)
	if apierrors.IsNotFound(err) {
		return nil, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to get machine for node name %s: %w", nodeName, err)
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
	klog.V(4).Infof("Getting node address for providerID: %s", providerID)
	machine, err := GetMachineForProviderID(ctx, o.onmetalClient, providerID)
	if apierrors.IsNotFound(err) {
		return []corev1.NodeAddress{}, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return []corev1.NodeAddress{}, err
	}
	return o.NodeAddresses(ctx, types.NodeName(machine.Name))
}

func (o *onmetalInstances) InstanceID(ctx context.Context, nodeName types.NodeName) (string, error) {
	klog.V(4).Infof("Getting instanceID for node: %s", nodeName)
	machine, err := GetMachineForNodeName(ctx, o.onmetalClient, o.namespace, nodeName)
	if apierrors.IsNotFound(err) {
		return "", cloudprovider.InstanceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get machine for node name %s: %w", nodeName, err)
	}
	return string(machine.UID), nil
}

func (o *onmetalInstances) InstanceType(ctx context.Context, nodeName types.NodeName) (string, error) {
	klog.V(4).Infof("Getting instance type for node: %s", nodeName)
	machine, err := GetMachineForNodeName(ctx, o.onmetalClient, o.namespace, nodeName)
	if apierrors.IsNotFound(err) {
		return "", cloudprovider.InstanceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get machine for node name %s: %w", nodeName, err)
	}
	return machine.Spec.MachineClassRef.Name, nil
}

func (o *onmetalInstances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	klog.V(4).Infof("Getting instance type for providerID: %s", providerID)
	return o.InstanceType(ctx, types.NodeName(getMachineUIDFromProviderID(providerID)))
}

func (o *onmetalInstances) AddSSHKeyToAllInstances(_ context.Context, _ string, _ []byte) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalInstances) CurrentNodeName(_ context.Context, hostName string) (types.NodeName, error) {
	return types.NodeName(hostName), nil
}

func (o *onmetalInstances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	klog.V(4).Infof("Checking if instance exists for providerID: %s", providerID)
	_, err := GetMachineForProviderID(ctx, o.onmetalClient, providerID)
	if apierrors.IsNotFound(err) {
		return false, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return false, fmt.Errorf("unable to get machine for provider ID %s: %w", providerID, err)
	}
	return true, nil
}

func (o *onmetalInstances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	klog.V(4).Infof("Checking if instance with providerID %s is shut down", providerID)
	machine, err := GetMachineForProviderID(ctx, o.onmetalClient, providerID)
	if apierrors.IsNotFound(err) {
		return false, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return false, err
	}

	return machine.Status.State == v1alpha1.MachineStateShutdown, nil
}

func getMachineUIDFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, CloudProviderName+"://")
}
