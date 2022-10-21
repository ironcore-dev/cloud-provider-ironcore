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

	computev1alpha1 "github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetMachineForProviderID(ctx context.Context, c client.Client, providerID string) (*computev1alpha1.Machine, error) {
	machineUID := getMachineUIDFromProviderID(providerID)
	machineList := &computev1alpha1.MachineList{}
	if err := c.List(ctx, machineList, client.MatchingFields{
		machineMetadataUIDField: machineUID,
	}); err != nil {
		return nil, fmt.Errorf("failed to get machine with UID %s: %w", machineUID, err)
	}
	switch len(machineList.Items) {
	case 0:
		return nil, apierrors.NewNotFound(computev1alpha1.Resource("machines"), fmt.Sprintf("UID: %s", machineUID))
	case 1:
		return &machineList.Items[0], nil
	default:
		return nil, fmt.Errorf("multiple machines found for uid %s", machineUID)
	}
}

func GetMachineForNode(ctx context.Context, onmetalClient client.Client, namespace string, node *corev1.Node) (*computev1alpha1.Machine, error) {
	if node == nil {
		return nil, nil
	}
	return GetMachineForNodeName(ctx, onmetalClient, namespace, types.NodeName(node.Name))
}

func GetMachineForNodeName(ctx context.Context, client client.Client, namespace string, nodeName types.NodeName) (*computev1alpha1.Machine, error) {
	machine := &computev1alpha1.Machine{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: string(nodeName)}, machine)
	if apierrors.IsNotFound(err) {
		return nil, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, err
	}
	return machine, nil
}
