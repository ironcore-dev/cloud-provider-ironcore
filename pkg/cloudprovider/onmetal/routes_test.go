// Copyright 2023 OnMetal authors
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
	"fmt"

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("Routes", func() {
	ns, cp, network, clusterName := SetupTest()

	var (
		routesProvider cloudprovider.Routes
	)

	BeforeEach(func(ctx SpecContext) {
		By("setting the routes provider")
		var ok bool
		routesProvider, ok = (*cp).Routes()
		Expect(ok).To(BeTrue())
	})

	It("should list Routes for all network interfaces in current network", func(ctx SpecContext) {
		By("creating a machine object")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
				Labels:       map[string]string{LabelKeyClusterName: clusterName},
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machine)

		By("creating a network interface for machine")
		networkInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "networkinterface"),
				Labels:    map[string]string{LabelKeyClusterName: clusterName},
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs: []networkingv1alpha1.IPSource{{
					Value: commonv1alpha1.MustParseNewIP("100.0.0.1"),
				}},
				Prefixes: []networkingv1alpha1.PrefixSource{{
					Value: commonv1alpha1.MustParseNewIPPrefix("100.0.0.1/24"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("creating node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(machine.Namespace, machine.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("patching the addresses node status")
		nodeBase := node.DeepCopy()
		node.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "100.0.0.1",
			},
		}
		Expect(k8sClient.Status().Patch(ctx, node, client.MergeFrom(nodeBase))).To(Succeed())

		By("patching the network interface status to indicate availability and correct binding")
		networkInterfaceBase := networkInterface.DeepCopy()
		networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		networkInterface.Status.Phase = networkingv1alpha1.NetworkInterfacePhaseBound
		networkInterface.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("100.0.0.1/24")}
		Expect(k8sClient.Status().Patch(ctx, networkInterface, client.MergeFrom(networkInterfaceBase))).To(Succeed())

		By("patching the machine status to have a valid internal IP address")
		machineBase := machine.DeepCopy()
		machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
			{
				Name: "primary",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-%s", machine.Name, "primary"),
					},
				},
			},
		}
		machine.Status.State = computev1alpha1.MachineStateRunning
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
			Name:  "primary",
			Phase: computev1alpha1.NetworkInterfacePhaseBound,
			IPs:   []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")},
		}}
		Expect(k8sClient.Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("getting list of routes")
		Expect(routesProvider.ListRoutes(ctx, clusterName)).Should(Equal([]*cloudprovider.Route{{
			Name:            clusterName + "-" + "100.0.0.1/24",
			DestinationCIDR: "100.0.0.1/24",
			TargetNode:      types.NodeName(node.Name),
			TargetNodeAddresses: []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "100.0.0.1",
			}},
		}}))
	})

	It("should not list routes for network interface not having cluster name label", func(ctx SpecContext) {
		By("creating a machine object without cluster label")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machine)

		By("creating a network interface for machine without cluster label")
		networkInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "networkinterface"),
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs: []networkingv1alpha1.IPSource{{
					Value: commonv1alpha1.MustParseNewIP("100.0.0.1"),
				}},
				Prefixes: []networkingv1alpha1.PrefixSource{{
					Value: commonv1alpha1.MustParseNewIPPrefix("100.0.0.1/24"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("creating node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(machine.Namespace, machine.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("patching the addresses node status")
		nodeBase := node.DeepCopy()
		node.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "100.0.0.1",
			},
		}
		Expect(k8sClient.Status().Patch(ctx, node, client.MergeFrom(nodeBase))).To(Succeed())

		By("patching the network interface status to indicate availability and correct binding")
		networkInterfaceBase := networkInterface.DeepCopy()
		networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		networkInterface.Status.Phase = networkingv1alpha1.NetworkInterfacePhaseBound
		networkInterface.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("100.0.0.1/24")}
		Expect(k8sClient.Status().Patch(ctx, networkInterface, client.MergeFrom(networkInterfaceBase))).To(Succeed())

		By("patching the machine status to have a valid internal IP address")
		machineBase := machine.DeepCopy()
		machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
			{
				Name: "primary",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-%s", machine.Name, "primary"),
					},
				},
			},
		}
		machine.Status.State = computev1alpha1.MachineStateRunning
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
			Name:  "primary",
			Phase: computev1alpha1.NetworkInterfacePhaseBound,
			IPs:   []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")},
		}}
		Expect(k8sClient.Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("getting list of routes")
		var routes []*cloudprovider.Route
		Expect(routesProvider.ListRoutes(ctx, clusterName)).Should(Equal(routes))
	})

	It("should ensure that a prefix has been created for a route", func(ctx SpecContext) {
		By("creating a machine object")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
				Labels:       map[string]string{LabelKeyClusterName: clusterName},
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machine)

		By("creating a network interface for machine")
		networkInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "primary"),
				Labels:    map[string]string{LabelKeyClusterName: clusterName},
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs: []networkingv1alpha1.IPSource{{
					Value: commonv1alpha1.MustParseNewIP("100.0.0.1"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("creating node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(machine.Namespace, machine.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("patching the addresses node status")
		nodeBase := node.DeepCopy()
		node.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "100.0.0.1",
			},
		}
		Expect(k8sClient.Status().Patch(ctx, node, client.MergeFrom(nodeBase))).To(Succeed())

		By("patching the network interface status to indicate availability and correct binding")
		networkInterfaceBase := networkInterface.DeepCopy()
		networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		networkInterface.Status.Phase = networkingv1alpha1.NetworkInterfacePhaseBound
		Expect(k8sClient.Status().Patch(ctx, networkInterface, client.MergeFrom(networkInterfaceBase))).To(Succeed())

		By("patching the machine status to have a valid internal IP address")
		machineBase := machine.DeepCopy()
		machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
			{
				Name: "primary",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-%s", machine.Name, "primary"),
					},
				},
			},
		}
		machine.Status.State = computev1alpha1.MachineStateRunning
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
			Name:  "primary",
			Phase: computev1alpha1.NetworkInterfacePhaseBound,
			IPs:   []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")},
		}}
		Expect(k8sClient.Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("ensuring that the route is represented by a prefix in the network interface spec")
		Expect(routesProvider.CreateRoute(ctx, clusterName, "my-route", &cloudprovider.Route{
			Name:            "foo",
			TargetNode:      types.NodeName(node.Name),
			DestinationCIDR: "100.0.0.1/24",
			TargetNodeAddresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "100.0.0.1",
				},
			},
		})).To(Succeed())
		Eventually(Object(networkInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", ContainElement(networkingv1alpha1.PrefixSource{
				Value: commonv1alpha1.MustParseNewIPPrefix("100.0.0.1/24"),
			})),
		))

		By("patching the network interface status to have the correct prefix in status")
		networkInterfaceBase = networkInterface.DeepCopy()
		networkInterface.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("100.0.0.1/24")}
		Expect(k8sClient.Status().Patch(ctx, networkInterface, client.MergeFrom(networkInterfaceBase))).To(Succeed())

		By("deleting prefix for route")
		Expect(routesProvider.DeleteRoute(ctx, clusterName, &cloudprovider.Route{
			Name:            "foo",
			TargetNode:      types.NodeName(node.Name),
			DestinationCIDR: "100.0.0.1/24",
			TargetNodeAddresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "100.0.0.1",
				},
			},
		})).To(Succeed())

		var prefixSources []networkingv1alpha1.PrefixSource
		Eventually(Object(networkInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", Equal(prefixSources)),
		))
	})
})
