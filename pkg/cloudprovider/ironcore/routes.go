// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
)

type ironcoreRoutes struct {
	targetClient      client.Client
	ironcoreClient    client.Client
	ironcoreNamespace string
	cloudConfig       CloudConfig
}

func newIroncoreRoutes(targetClient client.Client, ironcoreClient client.Client, namespace string, cloudConfig CloudConfig) cloudprovider.Routes {
	return &ironcoreRoutes{
		targetClient:      targetClient,
		ironcoreClient:    ironcoreClient,
		ironcoreNamespace: namespace,
		cloudConfig:       cloudConfig,
	}
}

func (o ironcoreRoutes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	klog.V(2).InfoS("Listing routes for cluster", "Cluster", clusterName)

	// Retrieve network interfaces matching the given namespace, network, and cluster label
	networkInterfaces := &networkingv1alpha1.NetworkInterfaceList{}
	if err := o.ironcoreClient.List(ctx, networkInterfaces, client.InNamespace(o.ironcoreNamespace), client.MatchingFields{
		networkInterfaceSpecNetworkRefNameField: o.cloudConfig.NetworkName,
	}, client.MatchingLabels{
		LabelKeyClusterName: clusterName,
	}); err != nil {
		return nil, fmt.Errorf("failed to list network interfaces for cluster %s: %w", clusterName, err)
	}

	var routes []*cloudprovider.Route
	// Iterate over each network interface and compile route information based on its prefixes
	for _, nic := range networkInterfaces.Items {
		// Verify that the network interface is associated with a machine reference
		if nic.Spec.MachineRef != nil && nic.Spec.MachineRef.Name != "" {
			for _, prefix := range nic.Status.Prefixes {
				destinationCIDR := prefix.String()
				// Retrieve node addresses based on the machine reference name
				targetNodeAddresses, err := o.getTargetNodeAddresses(ctx, nic.Spec.MachineRef.Name)
				if err != nil {
					return nil, fmt.Errorf("failed to get target node addresses for machine %s: %w", nic.Spec.MachineRef.Name, err)
				}
				route := &cloudprovider.Route{
					Name:                clusterName + "-" + destinationCIDR,
					DestinationCIDR:     destinationCIDR,
					TargetNode:          types.NodeName(nic.Spec.MachineRef.Name),
					TargetNodeAddresses: targetNodeAddresses,
				}
				routes = append(routes, route)
			}
		}
	}

	klog.V(2).InfoS("Route listing completed", "Cluster", clusterName, "Network", o.cloudConfig.NetworkName, "RouteCount", len(routes))
	return routes, nil
}

func (o ironcoreRoutes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	klog.V(2).InfoS("Initiating CreateRoute operation", "Cluster", clusterName, "Route", route, "NameHint", nameHint)

	// Retrieve the machine object corresponding to the node name
	nodeName := string(route.TargetNode)
	machine := &computev1alpha1.Machine{}
	if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: nodeName}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Machine object for node not found, returning InstanceNotFound", "Node", nodeName)
			return cloudprovider.InstanceNotFound
		}
		return fmt.Errorf("failed to get machine object for node %s: %w", nodeName, err)
	}
	klog.V(3).InfoS("Machine object retrieval completed", "Node", nodeName)

	// Iterate over all target node addresses and identify the matching network interface using internal IPs
	for _, address := range route.TargetNodeAddresses {
		if address.Type == corev1.NodeInternalIP {
			klog.V(3).InfoS("Evaluating internal IP address", "Node", nodeName, "Address", address.Address)
			for _, networkInterface := range machine.Status.NetworkInterfaces {
				interfaceFound := false
				for _, p := range networkInterface.IPs {
					if p.String() == address.Address {
						interfaceFound = true
						break
					}
				}

				// If a matching network interface is found, proceed with prefix operations
				if interfaceFound {
					klog.V(3).InfoS("Matching network interface identified", "Node", nodeName, "Address", address.Address)
					networkInterfaceName := getNetworkInterfaceName(machine, networkInterface)
					nic := &networkingv1alpha1.NetworkInterface{}
					if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: networkInterfaceName}, nic); err != nil {
						return err
					}

					klog.V(3).InfoS("NetworkInterface retrieval completed", "NetworkInterface", networkInterfaceName)

					// Check if the prefix already exists in the network interface
					prefixExists := false
					for _, prefix := range nic.Status.Prefixes {
						if prefix.Prefix.String() == route.DestinationCIDR {
							prefixExists = true
							break
						}
					}

					// If the prefix is not found, add it to the network interface specification
					if !prefixExists {
						klog.V(3).InfoS("Prefix not detected, proceeding with addition", "NetworkInterface", networkInterfaceName, "Prefix", route.DestinationCIDR)
						nicBase := nic.DeepCopy()
						ipPrefix := commonv1alpha1.MustParseIPPrefix(route.DestinationCIDR)
						prefixSource := networkingv1alpha1.PrefixSource{
							Value: &ipPrefix,
						}
						nic.Spec.Prefixes = append(nic.Spec.Prefixes, prefixSource)

						klog.V(2).InfoS("Applying patch to NetworkInterface to incorporate new prefix", "NetworkInterface", networkInterfaceName, "Node", nodeName, "Prefix", route.DestinationCIDR)
						if err := o.ironcoreClient.Patch(ctx, nic, client.MergeFrom(nicBase)); err != nil {
							return fmt.Errorf("failed to patch NetworkInterface %s for Node %s: %w", networkInterfaceName, nodeName, err)
						}
						klog.V(2).InfoS("Patch applied to NetworkInterface", "NetworkInterface", networkInterfaceName, "Prefix", route.DestinationCIDR)
					} else {
						klog.V(3).InfoS("Prefix already present on NetworkInterface", "NetworkInterface", networkInterfaceName, "Prefix", route.DestinationCIDR)
					}
				} else {
					klog.V(4).InfoS("No matching network interface identified for IP address", "Node", nodeName, "Address", address.Address)
				}
			}
		} else {
			klog.V(4).InfoS("Ignoring non-internal IP address", "Node", nodeName, "AddressType", address.Type, "Address", address.Address)
		}
	}
	klog.V(2).InfoS("CreateRoute operation completed", "Cluster", clusterName, "Route", route, "NameHint", nameHint)
	return nil
}

func (o ironcoreRoutes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	klog.V(2).InfoS("Initiating route deletion", "Cluster", clusterName, "Route", route)

	// Retrieve the machine object based on the node name
	nodeName := string(route.TargetNode)
	machine := &computev1alpha1.Machine{}
	if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: nodeName}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return cloudprovider.InstanceNotFound
		}
		return fmt.Errorf("failed to retrieve machine object for node %s: %w", nodeName, err)
	}

	// Iterate over all target node addresses and find matching network interfaces by internal IP
	for _, address := range route.TargetNodeAddresses {
		if address.Type == corev1.NodeInternalIP {
			klog.V(3).InfoS("Evaluating internal IP address for network interface match", "Node", nodeName, "Address", address.Address)
			for _, networkInterface := range machine.Status.NetworkInterfaces {
				interfaceFound := false
				for _, p := range networkInterface.IPs {
					if p.String() == address.Address {
						interfaceFound = true
						break
					}
				}

				// If a matching network interface is found, attempt to remove the prefix
				if interfaceFound {
					networkInterfaceName := getNetworkInterfaceName(machine, networkInterface)
					nic := &networkingv1alpha1.NetworkInterface{}
					if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: networkInterfaceName}, nic); err != nil {
						return err
					}

					// Check for the prefix in the network interface's spec and remove it if present
					for i, prefix := range nic.Spec.Prefixes {
						if prefix.Value.String() == route.DestinationCIDR {
							nicBase := nic.DeepCopy()
							nic.Spec.Prefixes = append(nic.Spec.Prefixes[:i], nic.Spec.Prefixes[i+1:]...)
							klog.V(2).InfoS("Prefix identified and marked for removal", "Prefix", prefix.Value.String(), "UpdatedPrefixes", nic.Spec.Prefixes)

							if err := o.ironcoreClient.Patch(ctx, nic, client.MergeFrom(nicBase)); err != nil {
								return fmt.Errorf("failed to patch NetworkInterface %s for Node %s: %w", client.ObjectKeyFromObject(nic), nodeName, err)
							}
							klog.V(2).InfoS("Prefix removal patch applied", "NetworkInterface", networkInterfaceName, "Node", nodeName)
							break
						}
					}
				}
			}
		} else {
			klog.V(4).InfoS("Ignoring non-internal IP address", "Node", nodeName, "AddressType", address.Type, "Address", address.Address)
		}
	}

	klog.V(2).InfoS("Route deletion completed", "Cluster", clusterName, "Route", route)
	return nil
}

func getNetworkInterfaceName(machine *computev1alpha1.Machine, networkInterface computev1alpha1.NetworkInterfaceStatus) string {
	for _, nic := range machine.Spec.NetworkInterfaces {
		if nic.Name == networkInterface.Name {
			if nic.NetworkInterfaceRef != nil {
				return nic.NetworkInterfaceRef.Name
			}
		}
	}
	return fmt.Sprintf("%s-%s", machine.Name, networkInterface.Name)
}

func (o ironcoreRoutes) getTargetNodeAddresses(ctx context.Context, nodeName string) ([]corev1.NodeAddress, error) {
	node := &corev1.Node{}
	if err := o.targetClient.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return nil, fmt.Errorf("failed to get node object %s: %w", nodeName, err)
	}
	return node.Status.Addresses, nil
}
