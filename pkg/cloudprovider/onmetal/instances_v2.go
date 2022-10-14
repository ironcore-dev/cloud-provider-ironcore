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

	"github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalInstancesV2 struct {
	onmetalClient client.Client
	targetClient  client.Client
}

func newOnmetalInstancesV2(onmetalClient client.Client, targetClient client.Client) cloudprovider.InstancesV2 {
	return &onmetalInstancesV2{
		onmetalClient: onmetalClient,
		targetClient:  targetClient,
	}
}

func (o *onmetalInstancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	klog.V(4).Infof("Checking if instance exists for node: %s", node.Name)
	_, err := GetMachineForNode(ctx, o.onmetalClient, o.targetClient, node)
	if apierrors.IsNotFound(err) {
		return false, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return false, fmt.Errorf("unable to get machine for node %s: %w", node.Name, err)
	}
	return true, nil
}

func (o *onmetalInstancesV2) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	klog.V(4).Infof("Checking if instance is shut down for node: %s", node.Name)
	machine, err := GetMachineForNode(ctx, o.onmetalClient, o.targetClient, node)
	if apierrors.IsNotFound(err) {
		return false, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return false, fmt.Errorf("failed to get machine for node %s: %w", node.Name, err)
	}

	return machine.Status.State == v1alpha1.MachineStateShutdown, nil
}

func (o *onmetalInstancesV2) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	if node == nil {
		return nil, nil
	}
	machine, err := GetMachineForNode(ctx, o.onmetalClient, o.targetClient, node)
	if apierrors.IsNotFound(err) {
		return nil, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get machine for node %s: %w", node.Name, err)
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
			addresses = append(addresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: ip.String(),
			})
		}
	}

	// TODO: handle zone and region
	return &cloudprovider.InstanceMetadata{
		ProviderID:    node.Spec.ProviderID,
		InstanceType:  machine.Spec.MachineClassRef.Name,
		NodeAddresses: addresses,
		Zone:          "",
		Region:        "",
	}, nil
}
