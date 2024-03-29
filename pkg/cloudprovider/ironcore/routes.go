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
	klog.V(2).InfoS("List Routes", "Cluster", clusterName)

	networkInterfaces := &networkingv1alpha1.NetworkInterfaceList{}
	if err := o.ironcoreClient.List(ctx, networkInterfaces, client.InNamespace(o.ironcoreNamespace), client.MatchingFields{
		networkInterfaceSpecNetworkRefNameField: o.cloudConfig.NetworkName,
	}, client.MatchingLabels{
		LabelKeyClusterName: clusterName,
	}); err != nil {
		return nil, err
	}

	var routes []*cloudprovider.Route
	// iterate over all network interfaces and collect all the prefixes as Routes
	for _, nic := range networkInterfaces.Items {
		if nic.Spec.MachineRef != nil && nic.Spec.MachineRef.Name != "" {
			for _, prefix := range nic.Status.Prefixes {
				destinationCIDR := prefix.String()
				targetNodeAddresses, err := o.getTargetNodeAddresses(ctx, nic.Spec.MachineRef.Name)
				if err != nil {
					return nil, err
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

	klog.V(2).InfoS("Current Routes", "Cluster", clusterName, "Network", o.cloudConfig.NetworkName, "Routes", routes)
	return routes, nil
}

func (o ironcoreRoutes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	klog.V(2).InfoS("Creating Route", "Cluster", clusterName, "Route", route, "NameHint", nameHint)

	// get the machine object based on the node name
	nodeName := string(route.TargetNode)
	machine := &computev1alpha1.Machine{}
	if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: nodeName}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return cloudprovider.InstanceNotFound
		}
		return fmt.Errorf("failed to get machine object for node %s: %w", nodeName, err)
	}

	// loop over all node addresses and find based on internal IP the matching network interface
	for _, address := range route.TargetNodeAddresses {
		// only use internal IP addresses
		if address.Type == corev1.NodeInternalIP {
			// loop over all network interfaces of the machine
			for _, networkInterface := range machine.Status.NetworkInterfaces {
				interfaceFound := false
				for _, p := range networkInterface.IPs {
					if p.String() == address.Address {
						interfaceFound = true
						break
					}
				}

				// if the interface is found, add the prefix to the network interface
				if interfaceFound {
					// get the network interface object
					networkInterfaceName := getNetworkInterfaceName(machine, networkInterface)
					nic := &networkingv1alpha1.NetworkInterface{}
					if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: networkInterfaceName}, nic); err != nil {
						return err
					}

					// check if the prefix is already added
					prefixExists := false
					for _, prefix := range nic.Status.Prefixes {
						if prefix.Prefix.String() == route.DestinationCIDR {
							prefixExists = true
							break
						}
					}

					// if the prefix is not added, add it
					if !prefixExists {
						nicBase := nic.DeepCopy()
						ipPrefix := commonv1alpha1.MustParseIPPrefix(route.DestinationCIDR)
						prefixSource := networkingv1alpha1.PrefixSource{
							Value: &ipPrefix,
						}
						nic.Spec.Prefixes = append(nic.Spec.Prefixes, prefixSource)

						klog.V(2).InfoS("Updating NetworkInterface by adding prefix", "NetworkInterface", client.ObjectKeyFromObject(nic), "Node", nodeName, "Prefix", route.DestinationCIDR)
						if err := o.ironcoreClient.Patch(ctx, nic, client.MergeFrom(nicBase)); err != nil {
							return fmt.Errorf("failed to patch NetworkInterface %s for Node %s: %w", client.ObjectKeyFromObject(nic), nodeName, err)
						}
					} else {
						klog.V(2).InfoS("NetworkInterface prefix already exists", "NetworkInterface", client.ObjectKeyFromObject(nic), "Node", nodeName, "Prefix", route.DestinationCIDR)
					}
				}
			}
		}
	}

	klog.V(2).InfoS("Created Route", "Cluster", clusterName, "Route", route, "NameHint", nameHint)

	return nil
}

func (o ironcoreRoutes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	klog.V(2).InfoS("Deleting Route", "Cluster", clusterName, "Route", route)

	// get the machine object based on the node name
	nodeName := string(route.TargetNode)
	machine := &computev1alpha1.Machine{}
	if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: nodeName}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return cloudprovider.InstanceNotFound
		}
		return fmt.Errorf("failed to get machine object for node %s: %w", nodeName, err)
	}

	// loop over all node addresses and find based on internal IP the matching network interface
	for _, address := range route.TargetNodeAddresses {
		// only use internal IP addresses
		if address.Type == corev1.NodeInternalIP {
			// loop over all network interfaces of the machine
			for _, networkInterface := range machine.Status.NetworkInterfaces {
				interfaceFound := false
				for _, p := range networkInterface.IPs {
					if p.String() == address.Address {
						interfaceFound = true
						break
					}
				}

				// if the interface is found, add the prefix to the network interface
				if interfaceFound {
					// get the network interface object
					networkInterfaceName := getNetworkInterfaceName(machine, networkInterface)
					nic := &networkingv1alpha1.NetworkInterface{}
					if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: networkInterfaceName}, nic); err != nil {
						return err
					}

					// check if the prefix exists
					for i, prefix := range nic.Status.Prefixes {
						if prefix.Prefix.String() == route.DestinationCIDR {
							nicBase := nic.DeepCopy()
							nic.Spec.Prefixes = append(nic.Spec.Prefixes[:i], nic.Spec.Prefixes[i+1:]...)
							klog.V(2).InfoS("Prefix found and removed", "Prefix", prefix.Prefix.String(), "Prefixes after", nic.Spec.Prefixes)

							if err := o.ironcoreClient.Patch(ctx, nic, client.MergeFrom(nicBase)); err != nil {
								return fmt.Errorf("failed to patch NetworkInterface %s for Node %s: %w", client.ObjectKeyFromObject(nic), nodeName, err)
							}

							break
						}
					}
				}
			}
		}
	}
	klog.V(2).InfoS("Deleted Route", "Cluster", clusterName, "Route", route)

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
