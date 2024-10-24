// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	cloudprovider "k8s.io/cloud-provider"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
)

var _ = Describe("LoadBalancer", func() {
	ns, cp, network, clusterName := SetupTest()

	var (
		lbProvider cloudprovider.LoadBalancer
	)

	BeforeEach(func(ctx SpecContext) {
		By("instantiating the load balancer provider")
		var ok bool
		lbProvider, ok = (*cp).LoadBalancer()
		Expect(ok).To(BeTrue())
	})

	It("should ensure external load balancer for service", func(ctx SpecContext) {
		By("creating a machine object")
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

		By("creating a network interface for machine")
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
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterface, func() {
			networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterface.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.1"),
			}
		})).Should(Succeed())

		By("patching the network interfaces of the machine")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "primary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterface.Name,
						},
					},
				},
			}
		})).Should(Succeed())

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

		By("creating test service of type load balancer")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "service-",
				Namespace:    ns.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{
					{
						Name:       "https",
						Protocol:   "TCP",
						Port:       443,
						TargetPort: intstr.IntOrString{IntVal: 443},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		DeferCleanup(k8sClient.Delete, service)

		// Start a goroutine to patch public IP into load banacer status in order to succeed EnsureLoadBalancer call
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      lbProvider.GetLoadBalancerName(ctx, clusterName, service),
			},
		}
		go func() {
			defer GinkgoRecover()
			By("patching public IP into load balancer status")
			Eventually(UpdateStatus(loadBalancer, func() {
				loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
			})).Should(Succeed())
		}()

		By("ensuring load balancer for service")
		Expect(lbProvider.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).Error().To(BeNil())

		By("ensuring the load balancer type is public and load balancer status has public IP")
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypePublic)),
			HaveField("Status.IPs", Equal([]commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}))))

		By("ensuring destinations of load balancer routing")
		lbRouting := &networkingv1alpha1.LoadBalancerRouting{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: service.Namespace,
				Name:      loadBalancer.Name,
			},
		}
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("ObjectMeta.OwnerReferences", ContainElement(metav1.OwnerReference{
				APIVersion: "networking.ironcore.dev/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       loadBalancer.Name,
				UID:        loadBalancer.UID,
			})),
			HaveField("Destinations", ContainElements([]networkingv1alpha1.LoadBalancerDestination{
				{
					IP: commonv1alpha1.MustParseIP("100.0.0.1"),
					TargetRef: &networkingv1alpha1.LoadBalancerTargetRef{
						UID:        networkInterface.UID,
						Name:       networkInterface.Name,
						ProviderID: networkInterface.Spec.ProviderID,
					},
				},
			})),
		))

		By("deleting the load balancer")
		Expect(lbProvider.EnsureLoadBalancerDeleted(ctx, clusterName, service)).To(Succeed())
	})

	It("should ensure an internal load balancer for service", func(ctx SpecContext) {
		By("creating a machine object")
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

		By("creating a network interface for machine")
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
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterface, func() {
			networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterface.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.1"),
			}
		})).Should(Succeed())

		By("patching the network interfaces of the machine")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "primary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterface.Name,
						},
					},
				},
			}
		})).Should(Succeed())

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

		By("creating test service of type internal load balancer")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "service-",
				Namespace:    ns.Name,
				Annotations: map[string]string{
					InternalLoadBalancerAnnotation: "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{
					{
						Name:       "https",
						Protocol:   "TCP",
						Port:       443,
						TargetPort: intstr.IntOrString{IntVal: 443},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		DeferCleanup(k8sClient.Delete, service)

		// Start a goroutine to patch internal IP into load banacer status in order to succeed EnsureLoadBalancer call
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      lbProvider.GetLoadBalancerName(ctx, clusterName, service),
			},
		}
		go func() {
			defer GinkgoRecover()
			By("patching internal IP into load balancer status")
			Eventually(UpdateStatus(loadBalancer, func() {
				loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.10")}
			})).Should(Succeed())
		}()

		By("ensuring load balancer for service")
		Expect(lbProvider.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).Error().To(BeNil())

		By("ensuring the load balancer type is internal and load balancer status has internal IP")
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypeInternal)),
			HaveField("Status.IPs", Equal([]commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.10")}))))

		By("removing internal load balancer annotation from service")
		Eventually(Update(service, func() {
			service.Annotations = map[string]string{}
		})).Should(Succeed())

		// Start a goroutine to patch public IP into load banacer status in order to succeed EnsureLoadBalancer call
		go func() {
			defer GinkgoRecover()
			By("patching public IP into LoadBalancer status")
			Eventually(UpdateStatus(loadBalancer, func() {
				loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
			})).Should(Succeed())
		}()

		By("ensuring load balancer for service")
		Expect(lbProvider.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).Error().To(BeNil())

		By("ensuring the load balancer type is public and load balancer status has public IP")
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypePublic)),
			HaveField("Status.IPs", Equal([]commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}))))

		By("ensuring destinations of load balancer routing")
		lbRouting := &networkingv1alpha1.LoadBalancerRouting{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: service.Namespace,
				Name:      loadBalancer.Name,
			},
		}
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("ObjectMeta.OwnerReferences", ContainElement(metav1.OwnerReference{
				APIVersion: "networking.ironcore.dev/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       loadBalancer.Name,
				UID:        loadBalancer.UID,
			})),
			HaveField("Destinations", ContainElements([]networkingv1alpha1.LoadBalancerDestination{
				{
					IP: commonv1alpha1.MustParseIP("100.0.0.1"),
					TargetRef: &networkingv1alpha1.LoadBalancerTargetRef{
						UID:        networkInterface.UID,
						Name:       networkInterface.Name,
						ProviderID: networkInterface.Spec.ProviderID,
					},
				},
			})),
		))

		By("getting load balancer for service")
		Expect(lbProvider.GetLoadBalancer(ctx, clusterName, service)).Error().NotTo(HaveOccurred())

		By("deleting the load balancer")
		Expect(lbProvider.EnsureLoadBalancerDeleted(ctx, clusterName, service)).To(Succeed())
	})

	It("should update LoadBalancer", func(ctx SpecContext) {
		By("creating a machine object")
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

		By("creating a network interface for machine")
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
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterface, func() {
			networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterface.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.1"),
			}
		})).Should(Succeed())

		By("creating a network interface for machine with wrong network")
		networkInterfaceFoo := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "networkinterfacefoo"),
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: "foo"},
				IPs: []networkingv1alpha1.IPSource{{
					Value: commonv1alpha1.MustParseNewIP("100.0.0.2"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterfaceFoo)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterfaceFoo)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterfaceFoo, func() {
			networkInterfaceFoo.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterfaceFoo.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.1"),
			}
		})).Should(Succeed())

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

		By("patching the network interfaces of the machine")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "primary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterface.Name,
						},
					},
				},
				{
					Name: "secondary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterfaceFoo.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		By("creating test service of type load balancer")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "service-",
				Namespace:    ns.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{
					{
						Name:       "https",
						Protocol:   "TCP",
						Port:       443,
						TargetPort: intstr.IntOrString{IntVal: 443},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		DeferCleanup(k8sClient.Delete, service)

		// Start a goroutine to patch public IP into load banacer status in order to succeed EnsureLoadBalancer call
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      lbProvider.GetLoadBalancerName(ctx, clusterName, service),
			},
		}
		go func() {
			defer GinkgoRecover()
			By("patching public IP into load balancer status")
			Eventually(UpdateStatus(loadBalancer, func() {
				loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
			})).Should(Succeed())
		}()

		By("ensuring load balancer for service")
		Expect(lbProvider.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).Error().To(BeNil())

		By("ensuring the load balancer type is public and load balancer status has public IP")
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypePublic)),
			HaveField("Status.IPs", Equal([]commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}))))

		By("creating a second machine object")
		machine2 := &computev1alpha1.Machine{
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
		Expect(k8sClient.Create(ctx, machine2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, machine2)

		By("creating a network interface for the second machine")
		networkInterface2 := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "networkinterface2"),
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				IPs: []networkingv1alpha1.IPSource{{
					Value: commonv1alpha1.MustParseNewIP("100.0.0.2"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine2.Name,
					UID:  machine2.UID,
				},
				ProviderID: "foo://bar",
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface2)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterface2, func() {
			networkInterface2.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterface2.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.2"),
			}
		})).Should(Succeed())

		By("patching the network interfaces of the machine")
		Eventually(Update(machine2, func() {
			machine2.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "primary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterface2.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		By("creating node object with a provider ID referencing the machine")
		node2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine2.Name,
			},
			Spec: corev1.NodeSpec{
				ProviderID: getProviderID(machine2.Namespace, machine2.Name),
			},
		}
		Expect(k8sClient.Create(ctx, node2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, node2)

		By("ensuring destinations of load balancer routing gets updated for node and node2")
		Expect(lbProvider.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node, node2})).NotTo(HaveOccurred())
		lbRouting := &networkingv1alpha1.LoadBalancerRouting{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: service.Namespace,
				Name:      loadBalancer.Name,
			},
		}
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("ObjectMeta.OwnerReferences", ContainElement(metav1.OwnerReference{
				APIVersion: "networking.ironcore.dev/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       loadBalancer.Name,
				UID:        loadBalancer.UID,
			})),
			// networkInterfaceFoo will not be listed in destinations, because network "foo" used by
			// networkInterfaceFoo does not exist
			HaveField("Destinations", ContainElements([]networkingv1alpha1.LoadBalancerDestination{
				{
					IP: commonv1alpha1.MustParseIP("100.0.0.1"),
					TargetRef: &networkingv1alpha1.LoadBalancerTargetRef{
						UID:        networkInterface.UID,
						Name:       networkInterface.Name,
						ProviderID: networkInterface.Spec.ProviderID,
					},
				}, {
					IP: commonv1alpha1.MustParseIP("100.0.0.2"),
					TargetRef: &networkingv1alpha1.LoadBalancerTargetRef{
						UID:        networkInterface2.UID,
						Name:       networkInterface2.Name,
						ProviderID: networkInterface2.Spec.ProviderID,
					},
				},
			})),
		))
	})

	It("should ensure LoadBalancer with legacy name", func(ctx SpecContext) {
		By("creating a machine object")
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

		By("creating a network interface for machine")
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
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterface)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterface, func() {
			networkInterface.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterface.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.1"),
			}
		})).Should(Succeed())

		By("creating a network interface for machine with network")
		networkInterfaceFoo := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      fmt.Sprintf("%s-%s", machine.Name, "networkinterfacefoo"),
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: "foo"},
				IPs: []networkingv1alpha1.IPSource{{
					Value: commonv1alpha1.MustParseNewIP("100.0.0.2"),
				}},
				MachineRef: &commonv1alpha1.LocalUIDReference{
					Name: machine.Name,
					UID:  machine.UID,
				},
			},
		}
		Expect(k8sClient.Create(ctx, networkInterfaceFoo)).To(Succeed())
		DeferCleanup(k8sClient.Delete, networkInterfaceFoo)

		By("patching the network interface status")
		Eventually(UpdateStatus(networkInterfaceFoo, func() {
			networkInterfaceFoo.Status.State = networkingv1alpha1.NetworkInterfaceStateAvailable
			networkInterfaceFoo.Status.IPs = []commonv1alpha1.IP{
				commonv1alpha1.MustParseIP("100.0.0.1"),
			}
		})).Should(Succeed())

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

		By("patching the network interfaces of the machine")
		Eventually(Update(machine, func() {
			machine.Spec.NetworkInterfaces = []computev1alpha1.NetworkInterface{
				{
					Name: "primary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterface.Name,
						},
					},
				},
				{
					Name: "secondary",
					NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
						NetworkInterfaceRef: &corev1.LocalObjectReference{
							Name: networkInterfaceFoo.Name,
						},
					},
				},
			}
		})).Should(Succeed())

		By("creating test service of type load balancer")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "service-",
				Namespace:    ns.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{
					{
						Name:       "https",
						Protocol:   "TCP",
						Port:       443,
						TargetPort: intstr.IntOrString{IntVal: 443},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		DeferCleanup(k8sClient.Delete, service)

		// Creating loadbalancer with legacy name
		nameSuffix := strings.Split(string(service.UID), "-")[0]
		lbName := fmt.Sprintf("%s-%s-%s", clusterName, service.Name, nameSuffix)
		var lbPorts []networkingv1alpha1.LoadBalancerPort
		for _, svcPort := range service.Spec.Ports {
			protocol := svcPort.Protocol
			lbPorts = append(lbPorts, networkingv1alpha1.LoadBalancerPort{
				Protocol: &protocol,
				Port:     svcPort.Port,
			})
		}
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			TypeMeta: metav1.TypeMeta{
				Kind:       "LoadBalancer",
				APIVersion: networkingv1alpha1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      lbName,
				Namespace: ns.Name,
				Annotations: map[string]string{
					AnnotationKeyClusterName:      clusterName,
					AnnotationKeyServiceName:      service.Name,
					AnnotationKeyServiceNamespace: service.Namespace,
					AnnotationKeyServiceUID:       string(service.UID),
				},
			},
			Spec: networkingv1alpha1.LoadBalancerSpec{
				Type:       networkingv1alpha1.LoadBalancerTypePublic,
				IPFamilies: service.Spec.IPFamilies,
				NetworkRef: v1.LocalObjectReference{
					Name: network.Name,
				},
				Ports: lbPorts,
			},
		}
		Expect(k8sClient.Create(ctx, loadBalancer)).To(Succeed())
		DeferCleanup(k8sClient.Delete, loadBalancer)

		go func() {
			defer GinkgoRecover()
			By("patching public IP into load balancer status")
			Eventually(UpdateStatus(loadBalancer, func() {
				loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
			})).Should(Succeed())
		}()

		By("ensuring load balancer for service")
		Expect(lbProvider.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).Error().To(BeNil())

		By("ensuring the load balancer type is public and load balancer status has public IP")
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("ObjectMeta.Name", Equal(lbName)),
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypePublic)),
			HaveField("Status.IPs", Equal([]commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}))))
	})

	It("should fail to get load balancer info if no load balancer is present", func(ctx SpecContext) {
		By("creating test service of type LoadBalancer")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "service-",
				Namespace:    ns.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{
					{
						Name:       "https",
						Protocol:   "TCP",
						Port:       443,
						TargetPort: intstr.IntOrString{IntVal: 443},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		DeferCleanup(k8sClient.Delete, service)

		By("ensuring that GetLoadBalancer returns instance not found for non existing object")
		_, exist, err := lbProvider.GetLoadBalancer(ctx, "foo", &corev1.Service{})
		Expect(err).To(HaveOccurred())
		Expect(exist).To(BeFalse())
	})
})
