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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
)

var _ = Describe("Routes", func() {

	var (
		route           *cloudprovider.Route
		node            *corev1.Node
		netInterface    *networkingv1alpha1.NetworkInterface
		oRoutes         cloudprovider.Routes
		destinationCIDR string
	)

	ns, _, network, clusterName := SetupTest()

	BeforeEach(func(ctx SpecContext) {
		By("creating machine")
		networkInterfaces := []computev1alpha1.NetworkInterface{
			{
				Name: "networkinterface",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: "machine-networkinterface",
					},
				},
			},
		}
		machine := newMachine(ns.Name, "machine", networkInterfaces)
		machine.Labels = map[string]string{LabeKeylClusterName: clusterName}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machine)

		By("creating a network interface for machine")
		netInterface = newNetworkInterface(ns.Name, "machine-networkinterface", network.Name, "10.0.0.5")
		netInterface.Labels = map[string]string{LabeKeylClusterName: clusterName}
		netInterface.Spec.MachineRef = &commonv1alpha1.LocalUIDReference{
			Name: machine.Name,
			UID:  machine.UID,
		}
		ipPrefix := commonv1alpha1.MustParseIPPrefix("10.0.0.5/32")
		netInterface.Spec.Prefixes = []networkingv1alpha1.PrefixSource{{
			Value: &ipPrefix,
		},
		}
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface)

		By("creating a network interface2 for machine without cluster name label")
		netInterface2 := newNetworkInterface(ns.Name, "machine-networkinterface2", network.Name, "10.0.0.6")
		netInterface2.Spec.MachineRef = &commonv1alpha1.LocalUIDReference{
			Name: machine.Name,
			UID:  machine.UID,
		}
		ipPrefix2 := commonv1alpha1.MustParseIPPrefix("10.0.0.6/32")
		netInterface2.Spec.Prefixes = []networkingv1alpha1.PrefixSource{{
			Value: &ipPrefix2,
		},
		}
		Expect(k8sClient.Create(ctx, netInterface2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface2)

		By("creating node object with a provider ID referencing the machine1")
		node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(machine.Namespace, machine.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("patching the network interface status to have a valid internal IP address")
		netInterfaceBase := netInterface.DeepCopy()
		netInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		netInterface.Status.Phase = networkingv1alpha1.NetworkInterfacePhaseBound
		netInterface.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("10.0.0.5/32")}
		Expect(k8sClient.Status().Patch(ctx, netInterface, client.MergeFrom(netInterfaceBase))).To(Succeed())

		By("patching the machine status to have a valid internal IP address")
		machineBase := machine.DeepCopy()
		machine.Status.State = computev1alpha1.MachineStateRunning
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
			Name:  networkInterfaces[0].Name,
			Phase: computev1alpha1.NetworkInterfacePhaseBound,
			IPs:   []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.5")},
		}}
		Expect(k8sClient.Status().Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		destinationCIDR = "10.0.0.5/32"
		route = &cloudprovider.Route{
			Name:            clusterName + "-" + destinationCIDR,
			DestinationCIDR: destinationCIDR,
			TargetNode:      types.NodeName(node.Name),
			TargetNodeAddresses: []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "10.0.0.5",
			}},
		}

		By("getting the routes interface")
		var ok bool
		oRoutes, ok = cloudProvider.Routes()
		Expect(ok).To(BeTrue())
	})

	It("should list Routes for all network interfaces in current network", func(ctx SpecContext) {
		By("getting list of routes")
		routes := []*cloudprovider.Route{{
			Name:            clusterName + "-" + destinationCIDR,
			DestinationCIDR: destinationCIDR,
			TargetNode:      types.NodeName(node.Name),
		}}
		Expect(oRoutes.ListRoutes(ctx, clusterName)).Should(Equal(routes))
	})

	It("should not list Routes for Network Interface not having cluster name label", func(ctx SpecContext) {
		By("creating machine2")
		networkInterfaces := []computev1alpha1.NetworkInterface{
			{
				Name: "networkinterface2",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: "machine2-networkinterface2",
					},
				},
			},
		}
		machine2 := newMachine(ns.Name, "machine2", networkInterfaces)
		Expect(k8sClient.Create(ctx, machine2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machine2)

		By("creating a network interface2 for machine2")
		netInterface2 := newNetworkInterface(ns.Name, "machine2-networkinterface2", network.Name, "10.0.0.7")
		netInterface2.Spec.MachineRef = &commonv1alpha1.LocalUIDReference{
			Name: machine2.Name,
			UID:  machine2.UID,
		}
		ipPrefix := commonv1alpha1.MustParseIPPrefix("10.0.0.7/32")
		netInterface2.Spec.Prefixes = []networkingv1alpha1.PrefixSource{{
			Value: &ipPrefix,
		},
		}
		Expect(k8sClient.Create(ctx, netInterface2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface2)

		By("patching the network interface2 status to have a valid internal IP address")
		netInterface2Base := netInterface2.DeepCopy()
		netInterface2.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		netInterface2.Status.Phase = networkingv1alpha1.NetworkInterfacePhaseBound
		netInterface2.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("10.0.0.7/32")}
		Expect(k8sClient.Status().Patch(ctx, netInterface2, client.MergeFrom(netInterface2Base))).To(Succeed())

		By("patching the machine status to have a valid internal IP address")
		machine2Base := machine2.DeepCopy()
		machine2.Status.State = computev1alpha1.MachineStateRunning
		machine2.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
			Name:  networkInterfaces[0].Name,
			Phase: computev1alpha1.NetworkInterfacePhaseBound,
			IPs:   []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.7")},
		}}
		Expect(k8sClient.Status().Patch(ctx, machine2, client.MergeFrom(machine2Base))).To(Succeed())

		By("getting list of routes")
		routes := []*cloudprovider.Route{{
			Name:            clusterName + "-" + destinationCIDR,
			DestinationCIDR: destinationCIDR,
			TargetNode:      types.NodeName(node.Name),
		}}
		Expect(oRoutes.ListRoutes(ctx, clusterName)).Should(Equal(routes))
	})

	It("should add/check prefix for Route", func(ctx SpecContext) {
		By("ensuring network interface prefix already exists")
		Expect(oRoutes.CreateRoute(ctx, clusterName, "my-route", route)).To(Succeed())
		Eventually(Object(netInterface)).Should(SatisfyAll(
			HaveField("Name", "machine-networkinterface"),
			HaveField("Status.Prefixes", []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix(destinationCIDR)}),
		))

		By("creating new prefix for Route")
		route.DestinationCIDR = "10.0.0.3/32" //assign new CIDR to create new prefix
		Expect(oRoutes.CreateRoute(ctx, clusterName, "my-route", route)).To(Succeed())
		ipPrefix := commonv1alpha1.MustParseIPPrefix(route.DestinationCIDR)
		Eventually(Object(netInterface)).Should(SatisfyAll(
			HaveField("Name", "machine-networkinterface"),
			HaveField("Spec.Prefixes", ContainElement(SatisfyAll(
				HaveField("Value", &ipPrefix),
			))),
		))
	})

	It("should delete prefix for Route", func(ctx SpecContext) {
		By("deleting prefix for Route")
		Expect(oRoutes.DeleteRoute(ctx, clusterName, route)).To(Succeed())
		var ipPrefix []networkingv1alpha1.PrefixSource
		Eventually(Object(netInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", ipPrefix),
		))
	})
})
