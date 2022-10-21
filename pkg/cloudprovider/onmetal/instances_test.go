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
	"fmt"
	"net/netip"

	commonv1alpha1 "github.com/onmetal/onmetal-api/apis/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/apis/networking/v1alpha1"
	"github.com/onmetal/onmetal-api/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Instances", func() {
	ctx := testutils.SetupContext()
	ns := SetupTest(ctx)

	It("Should get instance info", func() {
		By("creating a network")
		network := &networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "network-",
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())

		By("creating a machine")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				Image:           "my-image:latest",
				NetworkInterfaces: []computev1alpha1.NetworkInterface{
					{
						Name: "my-nic",
						NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
							Ephemeral: &computev1alpha1.EphemeralNetworkInterfaceSource{
								NetworkInterfaceTemplate: &networkingv1alpha1.NetworkInterfaceTemplateSpec{
									Spec: networkingv1alpha1.NetworkInterfaceSpec{
										NetworkRef: corev1.LocalObjectReference{Name: network.Name},
										IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.1")}},
										VirtualIP: &networkingv1alpha1.VirtualIPSource{
											Ephemeral: &networkingv1alpha1.EphemeralVirtualIPSource{
												VirtualIPTemplate: &networkingv1alpha1.VirtualIPTemplateSpec{
													Spec: networkingv1alpha1.VirtualIPSpec{
														Type:     networkingv1alpha1.VirtualIPTypePublic,
														IPFamily: corev1.IPv4Protocol,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Volumes: []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

		By("patching the machine status to have a valid virtual IP and internal IP interface address")
		machineBase := machine.DeepCopy()
		machine.Status.State = computev1alpha1.MachineStateRunning
		machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
			Name:      "my-nic",
			Phase:     computev1alpha1.NetworkInterfacePhaseBound,
			IPs:       []commonv1alpha1.IP{{Addr: netip.MustParseAddr("10.0.0.1")}},
			VirtualIP: &commonv1alpha1.IP{Addr: netip.MustParseAddr("10.0.0.10")},
		}}
		Expect(k8sClient.Status().Patch(ctx, machine, client.MergeFrom(machineBase))).To(Succeed())

		By("creating a node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())

		By("getting the instances interface")
		instances, ok := provider.Instances()
		Expect(ok).To(BeTrue())

		By("ensuring that the node address list contains the hostname, internal and external IP")
		Eventually(func(g Gomega) {
			addresses, err := instances.NodeAddresses(ctx, types.NodeName(node.Name))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(addresses).To(ContainElement(corev1.NodeAddress{
				Type:    corev1.NodeHostName,
				Address: machine.Name}))
			g.Expect(addresses).To(ContainElement(corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: "10.0.0.1"}))
			g.Expect(addresses).To(ContainElement(corev1.NodeAddress{
				Type:    corev1.NodeExternalIP,
				Address: "10.0.0.10"}))
		}).Should(Succeed())

		By("ensuring that the node address list derived from the provider ID contains the hostname, internal and external IP")
		Eventually(func(g Gomega) {
			addresses, err := instances.NodeAddressesByProviderID(ctx, fmt.Sprintf("%s://%s", CloudProviderName, machine.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(addresses).To(ContainElement(corev1.NodeAddress{
				Type:    corev1.NodeHostName,
				Address: machine.Name}))
			g.Expect(addresses).To(ContainElement(corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: "10.0.0.1"}))
			g.Expect(addresses).To(ContainElement(corev1.NodeAddress{
				Type:    corev1.NodeExternalIP,
				Address: "10.0.0.10"}))
		}).Should(Succeed())

		By("ensuring that the instance id is the machine name")
		Eventually(func(g Gomega) {
			nodeID, err := instances.InstanceID(ctx, types.NodeName(machine.Name))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodeID).To(Equal(string(machine.UID)))
		}).Should(Succeed())

		By("ensuring that the instance type is the machine class name")
		Eventually(func(g Gomega) {
			instanceType, err := instances.InstanceType(ctx, types.NodeName(machine.Name))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(instanceType).To(Equal(machine.Spec.MachineClassRef.Name))
		}).Should(Succeed())

		By("ignoring the ssh key for all instances is not implemented")
		err := instances.AddSSHKeyToAllInstances(ctx, "", []byte{})
		Expect(err).To(Equal(cloudprovider.NotImplemented))

		By("ensuring that the current node name is the machine name")
		Eventually(func(g Gomega) {
			nodeNameTyped, err := instances.CurrentNodeName(ctx, machine.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodeNameTyped).To(Equal(types.NodeName(machine.Name)))
		}).Should(Succeed())

		By("ensuring that the instance can be found by it's provider ID")
		Eventually(func(g Gomega) {
			exists, err := instances.InstanceExistsByProviderID(ctx, fmt.Sprintf("%s://%s", CloudProviderName, machine.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(exists).To(BeTrue())
		}).Should(Succeed())

		By("ensuring that the instance is not shut down")
		Eventually(func(g Gomega) {
			shutdown, err := instances.InstanceShutdownByProviderID(ctx, fmt.Sprintf("%s://%s", CloudProviderName, machine.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(shutdown).To(BeFalse())
		}).Should(Succeed())
	})

	It("Should not get instance for not supported provider ID", func() {
		By("creating a node object with an unsupported provider and wrong ID")
		const wrongProvider = "fake"
		const wrongInstanceID = "12345"
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "my-node-", // no matching machine should be available
			},
			Spec: corev1.NodeSpec{
				ProviderID: fmt.Sprintf("%s://%s", wrongProvider, wrongInstanceID),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())

		By("getting the instances interface")
		instances, ok := provider.Instances()
		Expect(ok).To(BeTrue())

		By("ensuring that the node address list is empty")
		Eventually(func(g Gomega) {
			addresses, err := instances.NodeAddresses(ctx, types.NodeName(node.Name))
			g.Expect(err).To(HaveOccurred())
			g.Expect(addresses).To(BeEmpty())
		}).Should(Succeed())

		By("ensuring that the node address for provider ID returns an empty list")
		Eventually(func(g Gomega) {
			addresses, err := instances.NodeAddressesByProviderID(ctx, fmt.Sprintf("%s://%s", wrongProvider, wrongInstanceID))
			g.Expect(err).To(Equal(cloudprovider.InstanceNotFound))
			g.Expect(addresses).To(BeEmpty())
		}).Should(Succeed())

		By("ensuring that the instance ID can not be found")
		Eventually(func(g Gomega) {
			nodeID, err := instances.InstanceID(ctx, types.NodeName(node.Name))
			g.Expect(err).To(HaveOccurred())
			g.Expect(nodeID).To(Equal(""))
		}).Should(Succeed())

		By("ensuring that the instance type is empty")
		Eventually(func(g Gomega) {
			instanceType, err := instances.InstanceType(ctx, types.NodeName(node.Name))
			g.Expect(err).To(HaveOccurred())
			g.Expect(instanceType).To(Equal(""))
		}).Should(Succeed())

		By("ensuring that the instance can not be found by it's provider ID")
		Eventually(func(g Gomega) {
			exists, err := instances.InstanceExistsByProviderID(ctx, fmt.Sprintf("%s://%s", wrongProvider, wrongInstanceID))
			g.Expect(err).To(HaveOccurred())
			g.Expect(exists).To(BeFalse())
		}).Should(Succeed())

		By("ensuring that the non existing instance is shut down")
		Eventually(func(g Gomega) {
			shutdown, err := instances.InstanceShutdownByProviderID(ctx, fmt.Sprintf("%s://%s", wrongProvider, wrongInstanceID))
			g.Expect(err).To(HaveOccurred())
			g.Expect(shutdown).To(BeFalse())
		}).Should(Succeed())
	})

	It("Should not get instance for when a wrong instance ID is provided", func() {
		By("creating a node object with a wrong instance ID")
		const wrongInstanceID = "12345"
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "my-node-", // no matching machine should be available
			},
			Spec: corev1.NodeSpec{
				ProviderID: fmt.Sprintf("%s://%s", CloudProviderName, wrongInstanceID),
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())

		By("getting the instances interface")
		instances, ok := provider.Instances()
		Expect(ok).To(BeTrue())

		By("ensuring that the node address list is empty")
		Eventually(func(g Gomega) {
			addresses, err := instances.NodeAddresses(ctx, types.NodeName(node.Name))
			g.Expect(err).To(HaveOccurred())
			g.Expect(addresses).To(BeEmpty())
		}).Should(Succeed())

		By("ensuring that the node address for provider ID returns an empty list")
		Eventually(func(g Gomega) {
			addresses, err := instances.NodeAddressesByProviderID(ctx, fmt.Sprintf("%s://%s", CloudProviderName, wrongInstanceID))
			g.Expect(err).To(Equal(cloudprovider.InstanceNotFound))
			g.Expect(addresses).To(BeEmpty())
		}).Should(Succeed())

		By("ensuring that the instance ID can not be found")
		Eventually(func(g Gomega) {
			nodeID, err := instances.InstanceID(ctx, types.NodeName(node.Name))
			g.Expect(err).To(HaveOccurred())
			g.Expect(nodeID).To(Equal(""))
		}).Should(Succeed())

		By("ensuring that the instance type is empty")
		Eventually(func(g Gomega) {
			instanceType, err := instances.InstanceType(ctx, types.NodeName(node.Name))
			g.Expect(err).To(HaveOccurred())
			g.Expect(instanceType).To(Equal(""))
		}).Should(Succeed())

		By("ensuring that the instance can not be found by it's provider ID")
		Eventually(func(g Gomega) {
			exists, err := instances.InstanceExistsByProviderID(ctx, fmt.Sprintf("%s://%s", CloudProviderName, wrongInstanceID))
			g.Expect(err).To(HaveOccurred())
			g.Expect(exists).To(BeFalse())
		}).Should(Succeed())

		By("ensuring that the non existing instance is shut down")
		Eventually(func(g Gomega) {
			shutdown, err := instances.InstanceShutdownByProviderID(ctx, fmt.Sprintf("%s://%s", CloudProviderName, wrongInstanceID))
			g.Expect(err).To(HaveOccurred())
			g.Expect(shutdown).To(BeFalse())
		}).Should(Succeed())
	})
})
