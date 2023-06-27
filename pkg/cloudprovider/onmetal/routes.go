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
	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalRoutes struct {
	targetClient     client.Client
	onmetalClient    client.Client
	onmetalNamespace string
	cloudConfig      CloudConfig
}

func newOnmetalRoutes(targetClient client.Client, onmetalClient client.Client, namespace string, cloudConfig CloudConfig) cloudprovider.Routes {
	return &onmetalRoutes{
		targetClient:     targetClient,
		onmetalClient:    onmetalClient,
		onmetalNamespace: namespace,
		cloudConfig:      cloudConfig,
	}
}

func (o onmetalRoutes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	klog.V(2).InfoS("List Routes", "Cluster", clusterName, "Network", o.cloudConfig.NetworkName)

	// get all network interfaces for the network
	nics := &networkingv1alpha1.NetworkInterfaceList{}
	err := o.onmetalClient.List(ctx, nics, client.InNamespace(o.onmetalNamespace), client.MatchingFields{
		networkInterfaceSpecNetworkRefNameField: o.cloudConfig.NetworkName,
	})

	if err != nil {
		return nil, err
	}

	var routes []*cloudprovider.Route
	// iterate over all network interfaces and create a route for each prefix
	for _, nic := range nics.Items {
		if nic.Spec.MachineRef != nil && nic.Spec.MachineRef.Name != "" {
			for _, prefix := range nic.Status.Prefixes {
				destinationCIDR := prefix.String()
				route := &cloudprovider.Route{
					Name:            clusterName + "-" + destinationCIDR,
					DestinationCIDR: destinationCIDR,
					TargetNode:      types.NodeName(nic.Spec.MachineRef.Name),
				}

				routes = append(routes, route)
			}
		}
	}

	klog.V(2).InfoS("Current Routes", "Cluster", clusterName, "Network", o.cloudConfig.NetworkName, "Routes", routes)
	return routes, nil
}

func (o onmetalRoutes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	klog.V(2).InfoS("Create Route", "Cluster", clusterName, "Route", route, "NameHint", nameHint)

	// get the machine object based on the node name
	node := string(route.TargetNode)
	machine := &computev1alpha1.Machine{}
	if err := o.onmetalClient.Get(ctx, client.ObjectKey{Namespace: o.onmetalNamespace, Name: node}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return cloudprovider.InstanceNotFound
		}
		return fmt.Errorf("failed to get machine object for node %s: %w", node, err)
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
					networkInterfaceName := fmt.Sprintf("%s-%s", machine.Name, networkInterface.Name)
					nic := &networkingv1alpha1.NetworkInterface{}
					err := o.onmetalClient.Get(ctx, client.ObjectKey{Namespace: o.onmetalNamespace, Name: networkInterfaceName}, nic)
					if err != nil {
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

						klog.V(2).InfoS("Updating NetworkInterface by adding prefix", "NetworkInterface", client.ObjectKeyFromObject(nic), "Node", node, "Prefix", route.DestinationCIDR)
						if err := o.onmetalClient.Patch(ctx, nic, client.MergeFrom(nicBase)); err != nil {
							return fmt.Errorf("failed to patch NetworkInterface %s for Node %s: %w", client.ObjectKeyFromObject(nic), node, err)
						}
					} else {
						klog.V(2).InfoS("NetworkInterface prefix already exists", "NetworkInterface", client.ObjectKeyFromObject(nic), "Node", node, "Prefix", route.DestinationCIDR)
					}
				}
			}
		}
	}

	return nil
}

func (o onmetalRoutes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	klog.V(2).InfoS("Delete Route", "Cluster", clusterName, "Route", route)

	// get the machine object based on the node name
	node := string(route.TargetNode)
	machine := &computev1alpha1.Machine{}
	if err := o.onmetalClient.Get(ctx, client.ObjectKey{Namespace: o.onmetalNamespace, Name: node}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return cloudprovider.InstanceNotFound
		}
		return fmt.Errorf("failed to get machine object for node %s: %w", node, err)
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
					networkInterfaceName := fmt.Sprintf("%s-%s", machine.Name, networkInterface.Name)
					nic := &networkingv1alpha1.NetworkInterface{}
					err := o.onmetalClient.Get(ctx, client.ObjectKey{Namespace: o.onmetalNamespace, Name: networkInterfaceName}, nic)
					if err != nil {
						return err
					}

					// check if the prefix exists
					for i, prefix := range nic.Status.Prefixes {
						if prefix.Prefix.String() == route.DestinationCIDR {
							nicBase := nic.DeepCopy()
							nic.Spec.Prefixes = nic.Spec.Prefixes[:i+copy(nic.Spec.Prefixes[i:], nic.Spec.Prefixes[i+1:])]

							klog.V(2).InfoS("Updating NetworkInterface by removing prefix", "NetworkInterface", client.ObjectKeyFromObject(nic), "Node", node, "Prefix", route.DestinationCIDR)
							if err := o.onmetalClient.Patch(ctx, nic, client.MergeFrom(nicBase)); err != nil {
								return fmt.Errorf("failed to patch NetworkInterface %s for Node %s: %w", client.ObjectKeyFromObject(nic), node, err)
							}

							break
						}
					}
				}
			}
		}
	}

	return nil
}
