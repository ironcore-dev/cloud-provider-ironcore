// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"fmt"

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
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
		machine        *computev1alpha1.Machine
		node           *corev1.Node
	)

	BeforeEach(func(ctx SpecContext) {
		By("setting the routes provider")
		var ok bool
		routesProvider, ok = (*cp).Routes()
		Expect(ok).To(BeTrue())

		By("creating a machine object")
		machine = &computev1alpha1.Machine{
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

		By("creating node object with a provider ID referencing the machine")
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
	})

	It("should list Routes for all network interfaces in current network", func(ctx SpecContext) {
		By("patching the machine with cluster name label")
		baseMachine := machine.DeepCopy()
		machine.ObjectMeta.Labels = map[string]string{LabelKeyClusterName: clusterName}
		Expect(k8sClient.Status().Patch(ctx, machine, client.MergeFrom(baseMachine))).To(Succeed())

		By("creating a static network interface for machine")
		staticNetworkInterface := &networkingv1alpha1.NetworkInterface{
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
				Prefixes: []networkingv1alpha1.PrefixSource{{
					Value: commonv1alpha1.MustParseNewIPPrefix("100.0.0.1/24"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, staticNetworkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, staticNetworkInterface)

		By("creating an ephemeral network interface for machine")
		ephemeralNetworkInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "ephemeral"),
				Labels:    map[string]string{LabelKeyClusterName: clusterName},
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs: []networkingv1alpha1.IPSource{
					{
						Ephemeral: &networkingv1alpha1.EphemeralPrefixSource{
							PrefixTemplate: &ipamv1alpha1.PrefixTemplateSpec{
								Spec: ipamv1alpha1.PrefixSpec{
									Prefix: commonv1alpha1.MustParseNewIPPrefix("192.168.0.1/32"),
								},
							},
						},
					},
				},
				IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, ephemeralNetworkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ephemeralNetworkInterface)

		By("patching the addresses to node status")
		nodeBase := node.DeepCopy()
		node.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "100.0.0.1",
			},
			{
				Type:    corev1.NodeInternalIP,
				Address: "192.168.0.1",
			},
		}
		Expect(k8sClient.Status().Patch(ctx, node, client.MergeFrom(nodeBase))).To(Succeed())

		By("patching the static network interface status to indicate availability and correct binding")
		staticNetworkInterfaceBase := staticNetworkInterface.DeepCopy()
		staticNetworkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		staticNetworkInterface.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("100.0.0.1/24")}
		Expect(k8sClient.Status().Patch(ctx, staticNetworkInterface, client.MergeFrom(staticNetworkInterfaceBase))).To(Succeed())

		By("patching the ephemeral network interface status to indicate availability and correct binding")
		ephemeralNetworkInterfaceBase := ephemeralNetworkInterface.DeepCopy()
		ephemeralNetworkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		ephemeralNetworkInterface.Status.Prefixes = []commonv1alpha1.IPPrefix{commonv1alpha1.MustParseIPPrefix("192.168.0.1/32")}
		Expect(k8sClient.Status().Patch(ctx, ephemeralNetworkInterface, client.MergeFrom(ephemeralNetworkInterfaceBase))).To(Succeed())

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
			{
				Name: "ephemeral",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-%s", machine.Name, "ephemeral"),
					},
				},
			},
		}
		machine.Status.State = computev1alpha1.MachineStateRunning
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{
			{
				Name: "primary",
				IPs:  []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")},
			},
			{
				Name: "ephemeral",
				IPs:  []commonv1alpha1.IP{commonv1alpha1.MustParseIP("192.168.0.1")},
			},
		}
		Expect(k8sClient.Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("getting list of routes")
		Expect(routesProvider.ListRoutes(ctx, clusterName)).Should(ConsistOf([]*cloudprovider.Route{
			{
				Name:            clusterName + "-" + "100.0.0.1/24",
				DestinationCIDR: "100.0.0.1/24",
				TargetNode:      types.NodeName(node.Name),
				TargetNodeAddresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "100.0.0.1",
					},
					{
						Type:    corev1.NodeInternalIP,
						Address: "192.168.0.1",
					},
				},
			},
			{
				Name:            clusterName + "-" + "192.168.0.1/32",
				DestinationCIDR: "192.168.0.1/32",
				TargetNode:      types.NodeName(node.Name),
				TargetNodeAddresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "100.0.0.1",
					},
					{
						Type:    corev1.NodeInternalIP,
						Address: "192.168.0.1",
					},
				},
			},
		}))
	})

	It("should not list routes for network interface not having cluster name label", func(ctx SpecContext) {
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
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("patching the addresses to node status")
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
			Name: "primary",
			IPs:  []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")},
		}}
		Expect(k8sClient.Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("getting list of routes")
		Expect(routesProvider.ListRoutes(ctx, clusterName)).Should(BeEmpty())
	})

	It("should ensure that a prefix has been created for a route", func(ctx SpecContext) {
		By("patching the machine with cluster name label")
		baseMachine := machine.DeepCopy()
		machine.ObjectMeta.Labels = map[string]string{LabelKeyClusterName: clusterName}
		Expect(k8sClient.Status().Patch(ctx, machine, client.MergeFrom(baseMachine))).To(Succeed())

		By("creating a static network interface for machine")
		staticNetworkInterface := &networkingv1alpha1.NetworkInterface{
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
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, staticNetworkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, staticNetworkInterface)

		By("creating an ephemeral network interface for machine")
		ephemeralNetworkInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "ephemeral"),
				Labels:    map[string]string{LabelKeyClusterName: clusterName},
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs: []networkingv1alpha1.IPSource{
					{
						Ephemeral: &networkingv1alpha1.EphemeralPrefixSource{
							PrefixTemplate: &ipamv1alpha1.PrefixTemplateSpec{
								Spec: ipamv1alpha1.PrefixSpec{
									Prefix: commonv1alpha1.MustParseNewIPPrefix("192.168.0.1/32"),
								},
							},
						},
					},
				},
				IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, ephemeralNetworkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ephemeralNetworkInterface)

		By("patching the addresses to node status")
		nodeBase := node.DeepCopy()
		node.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "100.0.0.1",
			},
			{
				Type:    corev1.NodeInternalIP,
				Address: "192.168.0.1",
			},
		}
		Expect(k8sClient.Status().Patch(ctx, node, client.MergeFrom(nodeBase))).To(Succeed())

		By("patching the static network interface status to indicate availability and correct binding")
		staticNetworkInterfaceBase := staticNetworkInterface.DeepCopy()
		staticNetworkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		Expect(k8sClient.Status().Patch(ctx, staticNetworkInterface, client.MergeFrom(staticNetworkInterfaceBase))).To(Succeed())

		By("patching the ephemeral network interface status to indicate availability and correct binding")
		ephemeralNetworkInterfaceBase := ephemeralNetworkInterface.DeepCopy()
		ephemeralNetworkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
		Expect(k8sClient.Status().Patch(ctx, ephemeralNetworkInterface, client.MergeFrom(ephemeralNetworkInterfaceBase))).To(Succeed())

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
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{
			{
				Name: "primary",
				IPs:  []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")},
			},
			{
				Name: "ephemeral",
				IPs:  []commonv1alpha1.IP{commonv1alpha1.MustParseIP("192.168.0.1")},
			},
		}
		Expect(k8sClient.Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("ensuring that the route is represented by a prefix in the static network interface spec")
		Expect(routesProvider.CreateRoute(ctx, clusterName, "my-route1", &cloudprovider.Route{
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

		Eventually(Object(staticNetworkInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", ContainElement(networkingv1alpha1.PrefixSource{
				Value: commonv1alpha1.MustParseNewIPPrefix("100.0.0.1/24"),
			})),
		))

		By("ensuring that the route is represented by a prefix in the ephemeral network interface spec")
		Expect(routesProvider.CreateRoute(ctx, clusterName, "my-route2", &cloudprovider.Route{
			Name:            "bar",
			TargetNode:      types.NodeName(node.Name),
			DestinationCIDR: "192.168.0.1/32",
			TargetNodeAddresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.0.1",
				},
			},
		})).To(Succeed())

		Eventually(Object(ephemeralNetworkInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", ContainElement(networkingv1alpha1.PrefixSource{
				Value: commonv1alpha1.MustParseNewIPPrefix("192.168.0.1/32"),
			})),
		))

		By("deleting prefix for static route")
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

		Eventually(Object(staticNetworkInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", BeEmpty()),
		))

		By("deleting prefix for ephemeral route")
		Expect(routesProvider.DeleteRoute(ctx, clusterName, &cloudprovider.Route{
			Name:            "bar",
			TargetNode:      types.NodeName(node.Name),
			DestinationCIDR: "192.168.0.1/32",
			TargetNodeAddresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.0.1",
				},
			},
		})).To(Succeed())

		Eventually(Object(ephemeralNetworkInterface)).Should(SatisfyAll(
			HaveField("Spec.Prefixes", BeEmpty()),
		))
	})
})
