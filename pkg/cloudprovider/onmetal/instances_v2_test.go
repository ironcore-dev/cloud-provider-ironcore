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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
)

var _ = Describe("InstancesV2", func() {
	ns, _, network := SetupTest()

	It("should get instance info", func(ctx SpecContext) {
		By("creating a machine")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				MachinePoolRef:  &corev1.LocalObjectReference{Name: "zone1"},
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
			IPs:       []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
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
		DeferCleanup(k8sClient.Delete, node)

		By("getting the instances v2 interface")
		instances, ok := cloudProvider.InstancesV2()
		Expect(ok).To(BeTrue())

		By("ensuring that an instance for a node exists")
		ok, err := instances.InstanceExists(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		By("ensuring that the instance is not shut down")
		ok, err = instances.InstanceShutdown(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("ensuring that the instance meta data has the correct addresses")
		instanceMetadata, err := instances.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Eventually(instanceMetadata).Should(SatisfyAll(
			HaveField("ProviderID", getProviderID(machine.Namespace, machine.Name)),
			HaveField("InstanceType", machine.Spec.MachineClassRef.Name),
			HaveField("NodeAddresses", ContainElements(
				corev1.NodeAddress{
					Type:    corev1.NodeExternalIP,
					Address: "10.0.0.10",
				},
				corev1.NodeAddress{
					Type:    corev1.NodeInternalIP,
					Address: "10.0.0.1",
				},
			)),
			HaveField("Zone", "zone1"),
			HaveField("Region", "")))

	})

	It("should get InstanceNotFound if no Machine exists for Node", func(ctx SpecContext) {
		By("creating a node object with a provider ID referencing non existing machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("getting the instances v2 interface")
		instances, ok := cloudProvider.InstancesV2()
		Expect(ok).To(BeTrue())

		By("ensuring that an instance for a node does not exist")
		ok, err := instances.InstanceExists(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(ok).To(BeFalse())
	})

	It("should fail to get instance metadata if no Machine exists for Node", func(ctx SpecContext) {
		By("creating a node object with a provider ID referencing non existing machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("getting the instances v2 interface")
		instances, ok := cloudProvider.InstancesV2()
		Expect(ok).To(BeTrue())

		By("ensuring to fail getting the instance metadata")
		metaData, err := instances.InstanceMetadata(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(metaData).To(BeNil())
	})

	It("should fail to get instance shutdown state if no Machine exists for Node", func(ctx SpecContext) {
		By("creating a node object with a provider ID referencing non existing machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("getting the instances v2 interface")
		instances, ok := cloudProvider.InstancesV2()
		Expect(ok).To(BeTrue())

		By("ensuring to fail getting the instance metadata")

		ok, err := instances.InstanceShutdown(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(ok).To(BeFalse())
	})
})

func getProviderID(namespace, machineName string) string {
	return fmt.Sprintf("%s://%s/%s", ProviderName, namespace, machineName)
}
