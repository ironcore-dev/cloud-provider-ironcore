// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"fmt"
	"net/netip"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cloudprovider "k8s.io/cloud-provider"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
)

var _ = Describe("InstancesV2", func() {
	var (
		instancesProvider cloudprovider.InstancesV2
	)
	ns, cp, network, clusterName := SetupTest()

	It("should get instance info", func(ctx SpecContext) {
		By("instantiating the instances v2 provider")
		var ok bool
		instancesProvider, ok = (*cp).InstancesV2()
		Expect(ok).To(BeTrue())

		By("creating a machine pool without topology annotations")
		machinePool := &computev1alpha1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "zone1",
			},
		}
		Expect(k8sClient.Create(ctx, machinePool)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machinePool)

		By("creating a machine")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				MachinePoolRef:  &corev1.LocalObjectReference{Name: machinePool.Name},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

		By("creating a network interface for machine")
		netInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-my-nic", machine.Name),
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.1")}},
				ProviderID: "foo://bar",
				VirtualIP: &networkingv1alpha1.VirtualIPSource{
					Ephemeral: &networkingv1alpha1.EphemeralVirtualIPSource{
						VirtualIPTemplate: &networkingv1alpha1.VirtualIPTemplateSpec{
							Spec: networkingv1alpha1.EphemeralVirtualIPSpec{
								VirtualIPSpec: networkingv1alpha1.VirtualIPSpec{
									Type:     networkingv1alpha1.VirtualIPTypePublic,
									IPFamily: corev1.IPv4Protocol,
								},
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface)

		By("patching the network interface status to indicate availability and correct binding")
		Eventually(UpdateStatus(netInterface, func() {
			netInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			netInterface.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
			netInterface.Status.VirtualIP = &commonv1alpha1.IP{Addr: netip.MustParseAddr("10.0.0.10")}
		})).Should(Succeed())

		By("patching the machine object to have a valid network interface ref, virtual IP and internal IP address")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "my-nic",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: netInterface.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		Eventually(UpdateStatus(machine, func() {
			machine.Status.State = computev1alpha1.MachineStateRunning
			machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
				Name: "my-nic",
				NetworkInterfaceRef: corev1.LocalObjectReference{
					Name: fmt.Sprintf("%s-my-nic", machine.Name),
				},
			}}
		})).Should(Succeed())

		By("creating a node object with a provider ID referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("ensuring that an instance for a node exists")
		ok, err := instancesProvider.InstanceExists(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		By("ensuring that the instance is not shut down")
		ok, err = instancesProvider.InstanceShutdown(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("ensuring that the instance meta data has the correct addresses")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
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

		By("ensuring cluster name label is added to Machine object")
		Eventually(Object(machine)).Should(SatisfyAll(
			HaveField("Labels", map[string]string{LabelKeyClusterName: clusterName}),
		))

		By("ensuring cluster name label is added to network interface of Machine object")
		Eventually(Object(netInterface)).Should(SatisfyAll(
			HaveField("Labels", map[string]string{LabelKeyClusterName: clusterName}),
		))

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

		By("ensuring that an instance for a node does not exist")
		ok, err := instancesProvider.InstanceExists(ctx, node)
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

		By("ensuring to fail getting the instance metadata")
		metaData, err := instancesProvider.InstanceMetadata(ctx, node)
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

		By("ensuring the shutdown state of a node")
		ok, err := instancesProvider.InstanceShutdown(ctx, node)
		Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		Expect(ok).To(BeFalse())
	})

	It("should return zone and region from machine pool annotations", func(ctx SpecContext) {
		By("instantiating the instances v2 provider")
		var ok bool
		instancesProvider, ok = (*cp).InstancesV2()
		Expect(ok).To(BeTrue())

		By("creating a machine pool with zone and region annotations")
		machinePool := &computev1alpha1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pool-with-zone-region",
				Annotations: map[string]string{
					string(commonv1alpha1.TopologyLabelZone):   "eu-west-1a",
					string(commonv1alpha1.TopologyLabelRegion): "eu-west-1",
				},
			},
		}
		Expect(k8sClient.Create(ctx, machinePool)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machinePool)

		By("creating a machine referencing the annotated pool")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				MachinePoolRef:  &corev1.LocalObjectReference{Name: machinePool.Name},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

		By("creating a network interface for machine")
		netInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-my-nic", machine.Name),
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.2")}},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(netInterface, func() {
			netInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			netInterface.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.2")}
		})).Should(Succeed())

		By("patching the machine object to have a valid network interface ref")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "my-nic",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: netInterface.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		Eventually(UpdateStatus(machine, func() {
			machine.Status.State = computev1alpha1.MachineStateRunning
			machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
				Name: "my-nic",
				NetworkInterfaceRef: corev1.LocalObjectReference{
					Name: netInterface.Name,
				},
			}}
		})).Should(Succeed())

		By("creating a node object referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("ensuring that instance metadata returns zone and region from pool annotations")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(instanceMetadata.Zone).To(Equal("eu-west-1a"))
		Expect(instanceMetadata.Region).To(Equal("eu-west-1"))
	})

	It("should return zone from annotation and empty region when only zone annotation is set", func(ctx SpecContext) {
		By("instantiating the instances v2 provider")
		var ok bool
		instancesProvider, ok = (*cp).InstancesV2()
		Expect(ok).To(BeTrue())

		By("creating a machine pool with only zone annotation")
		machinePool := &computev1alpha1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pool-zone-only",
				Annotations: map[string]string{
					string(commonv1alpha1.TopologyLabelZone): "us-east-1b",
				},
			},
		}
		Expect(k8sClient.Create(ctx, machinePool)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machinePool)

		By("creating a machine referencing the pool")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				MachinePoolRef:  &corev1.LocalObjectReference{Name: machinePool.Name},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

		By("creating a network interface for machine")
		netInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-my-nic", machine.Name),
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.3")}},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(netInterface, func() {
			netInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			netInterface.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.3")}
		})).Should(Succeed())

		By("patching the machine object to have a valid network interface ref")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "my-nic",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: netInterface.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		Eventually(UpdateStatus(machine, func() {
			machine.Status.State = computev1alpha1.MachineStateRunning
			machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
				Name: "my-nic",
				NetworkInterfaceRef: corev1.LocalObjectReference{
					Name: netInterface.Name,
				},
			}}
		})).Should(Succeed())

		By("creating a node object referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("ensuring that instance metadata returns zone from annotation and empty region")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(instanceMetadata.Zone).To(Equal("us-east-1b"))
		Expect(instanceMetadata.Region).To(BeEmpty())
	})

	It("should fall back zone to pool name when no topology annotations are set", func(ctx SpecContext) {
		By("instantiating the instances v2 provider")
		var ok bool
		instancesProvider, ok = (*cp).InstancesV2()
		Expect(ok).To(BeTrue())

		By("creating a machine pool without any annotations")
		machinePool := &computev1alpha1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pool-no-annotations",
			},
		}
		Expect(k8sClient.Create(ctx, machinePool)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machinePool)

		By("creating a machine referencing the pool")
		machine := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "machine-",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				MachinePoolRef:  &corev1.LocalObjectReference{Name: machinePool.Name},
				Image:           "my-image:latest",
				Volumes:         []computev1alpha1.Volume{},
			},
		}
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

		By("creating a network interface for machine")
		netInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-my-nic", machine.Name),
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.4")}},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, netInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(netInterface, func() {
			netInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			netInterface.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.4")}
		})).Should(Succeed())

		By("patching the machine object to have a valid network interface ref")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "my-nic",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: netInterface.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		Eventually(UpdateStatus(machine, func() {
			machine.Status.State = computev1alpha1.MachineStateRunning
			machine.Status.NetworkInterfaces = []computev1alpha1.NetworkInterfaceStatus{{
				Name: "my-nic",
				NetworkInterfaceRef: corev1.LocalObjectReference{
					Name: netInterface.Name,
				},
			}}
		})).Should(Succeed())

		By("creating a node object referencing the machine")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node)

		By("ensuring that instance metadata falls back zone to pool name and region is empty")
		instanceMetadata, err := instancesProvider.InstanceMetadata(ctx, node)
		Expect(err).NotTo(HaveOccurred())
		Expect(instanceMetadata.Zone).To(Equal("pool-no-annotations"))
		Expect(instanceMetadata.Region).To(BeEmpty())
	})
})

func getProviderID(namespace, machineName string) string {
	return fmt.Sprintf("%s://%s/%s", ProviderName, namespace, machineName)
}
