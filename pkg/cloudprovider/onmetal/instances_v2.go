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
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalInstancesV2 struct {
	client.Client
	namespace string
}

func newOnmetalInstancesV2(client client.Client, namespace string) cloudprovider.InstancesV2 {
	return &onmetalInstancesV2{
		Client:    client,
		namespace: namespace,
	}
}

func (o *onmetalInstancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	_, err := GetMachineForNode(ctx, o.Client, node)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "unable to get machine")
	}
	return true, nil
}

func (o *onmetalInstancesV2) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	machine, err := GetMachineForNode(ctx, o.Client, node)
	if err != nil {
		return false, fmt.Errorf("failed to get machine for node %s: %w", node.Name, err)
	}

	return machine.Status.State == v1alpha1.MachineStateShutdown, nil
}

func (o *onmetalInstancesV2) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	if node == nil {
		return nil, nil
	}
	machine, err := GetMachineForNode(ctx, o.Client, node)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine for node %s: %w", node.Name, err)
	}

	addresses := make([]corev1.NodeAddress, 0)
	for _, iface := range machine.Status.NetworkInterfaces {
		virtualIP := iface.VirtualIP.String()
		// TODO understand how to differentiate addresses
		addresses = append(addresses, corev1.NodeAddress{
			Type:    corev1.NodeInternalIP,
			Address: virtualIP,
		})
		for _, ip := range iface.IPs {
			ip := ip.String()
			addresses = append(addresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: ip,
			})
		}
	}

	// TODO understand how to get zones and regions
	meta := &cloudprovider.InstanceMetadata{
		ProviderID:    node.Spec.ProviderID,
		InstanceType:  machine.Spec.MachineClassRef.Name,
		NodeAddresses: addresses,
		Zone:          "",
		Region:        "",
	}

	return meta, nil
}
