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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetMachineForProviderID(ctx context.Context, c client.Client, providerID string) (*v1alpha1.Machine, error) {
	machineUID := getMachineUIDFromProviderID(providerID)
	machineList := &v1alpha1.MachineList{}
	if err := c.List(ctx, machineList, client.MatchingFields{
		machineMetadataUIDField: machineUID,
	}); err != nil {
		return nil, fmt.Errorf("failed to get machine with UID %s: %w", machineUID, err)
	}
	switch len(machineList.Items) {
	case 0:
		return nil, apierrors.NewNotFound(v1alpha1.Resource("machines"), fmt.Sprintf("UID: %s", machineUID))
	case 1:
		return &machineList.Items[0], nil
	default:
		return nil, fmt.Errorf("multiple machines found for uid %s", machineUID)
	}
}

func GetMachineForNode(ctx context.Context, c client.Client, node *corev1.Node) (*v1alpha1.Machine, error) {
	machine, err := GetMachineForNodeName(ctx, c, nil, types.NodeName(node.Name))
	if err != nil {
		return nil, err
	}
	return machine, err
}

func GetMachineForNodeName(ctx context.Context, onmetalClient client.Client, targetClient client.Client, nodeName types.NodeName) (*v1alpha1.Machine, error) {
	node, err := GetValidNodeForNodeName(ctx, targetClient, nodeName)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, nil
	}
	return GetMachineForProviderID(ctx, onmetalClient, node.Spec.ProviderID)
}

func GetValidNodeForNodeName(ctx context.Context, targetClient client.Client, nodeName types.NodeName) (*corev1.Node, error) {
	node := &corev1.Node{}
	if err := targetClient.Get(ctx, types.NamespacedName{Name: string(nodeName)}, node); err != nil {
		return nil, fmt.Errorf("failed to get node object %s: %w", nodeName, err)
	}
	if !IsValidProviderID(node.Spec.ProviderID) {
		// ignore invalid or empty provider IDs
		return nil, nil
	}
	return node, nil
}

func IsValidProviderID(id string) bool {
	return strings.HasPrefix(id, CloudProviderName)
}
