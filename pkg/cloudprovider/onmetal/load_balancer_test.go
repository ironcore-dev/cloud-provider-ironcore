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
	"net/netip"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	"github.com/onmetal/onmetal-api/testutils"
)

const clusterName = "dev"

var _ = Describe("LoadBalancer", func() {
	ctx := testutils.SetupContext()
	ns, networkName := SetupTest(ctx)

	It("Should ensure and update LoadBalancer info", func() {

		By("getting the LoadBalancer interface")
		loadbalancer, ok := provider.LoadBalancer()
		Expect(ok).To(BeTrue())

		By("creating a LoadBalancer type service")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-service",
				Namespace: ns.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{
					{
						Name:       "https",
						Protocol:   "TCP",
						Port:       443,
						TargetPort: intstr.IntOrString{IntVal: 443},
						NodePort:   31376,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())

		By("returning error if nodes are less than 1 for ensureloadbalancer")
		Eventually(func(g Gomega) {
			_, err := loadbalancer.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{})
			g.Expect(err).Should(MatchError("there are no available nodes for LoadBalancer service " + service.Name))
		}).Should(Succeed())

		By("creating a Node object")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())

		By("failing if no Machine is present for a given Node object")
		Eventually(func(g Gomega) {
			_, err := loadbalancer.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})
			g.Expect(err).Should(MatchError("instance not found"))
		}).Should(Succeed())

		By("creating a network interface")
		netInterface := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "machine1-netinterface",
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: networkName},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.5")}},
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
		}
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())

		By("creating a network interface")
		netInterface1 := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "machine2-netinterface1",
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: networkName},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.6")}},
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
		}
		Expect(k8sClient.Create(ctx, netInterface1)).To(Succeed())

		By("creating a network interface")
		netInterface2 := &networkingv1alpha1.NetworkInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "machine1-netinterface2",
				Namespace: ns.Name,
			},
			Spec: networkingv1alpha1.NetworkInterfaceSpec{
				NetworkRef: corev1.LocalObjectReference{Name: "my-network-2"},
				IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.7")}},
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
		}
		Expect(k8sClient.Create(ctx, netInterface2)).To(Succeed())

		By("creating a machine")
		machine1 := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      "machine1",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				Image:           "my-image:latest",

				NetworkInterfaces: []computev1alpha1.NetworkInterface{
					{
						Name: "netinterface",
						NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
							Ephemeral: &computev1alpha1.EphemeralNetworkInterfaceSource{
								NetworkInterfaceTemplate: &networkingv1alpha1.NetworkInterfaceTemplateSpec{
									Spec: networkingv1alpha1.NetworkInterfaceSpec{
										NetworkRef: corev1.LocalObjectReference{Name: networkName},
										IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.8")}},
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
					{
						Name: "netinterface2",
						NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
							Ephemeral: &computev1alpha1.EphemeralNetworkInterfaceSource{
								NetworkInterfaceTemplate: &networkingv1alpha1.NetworkInterfaceTemplateSpec{
									Spec: networkingv1alpha1.NetworkInterfaceSpec{
										NetworkRef: corev1.LocalObjectReference{Name: networkName},
										IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.9")}},
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
		Expect(k8sClient.Create(ctx, machine1)).To(Succeed())

		By("creating a machine")
		machine2 := &computev1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      "machine2",
			},
			Spec: computev1alpha1.MachineSpec{
				MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
				Image:           "my-image:latest",

				NetworkInterfaces: []computev1alpha1.NetworkInterface{
					{
						Name: "netinterface1",
						NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
							Ephemeral: &computev1alpha1.EphemeralNetworkInterfaceSource{
								NetworkInterfaceTemplate: &networkingv1alpha1.NetworkInterfaceTemplateSpec{
									Spec: networkingv1alpha1.NetworkInterfaceSpec{
										NetworkRef: corev1.LocalObjectReference{Name: networkName},
										IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP("10.0.0.6")}},
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

		Expect(k8sClient.Create(ctx, machine2)).To(Succeed())

		By("creating a Node object with a provider ID referencing the machine")
		node1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine1.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node1)).To(Succeed())

		By("creating a another Node object with a provider ID referencing the machine")
		node2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine2.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node2)).To(Succeed())

		By("failing when network object is not found")
		Eventually(func(g Gomega) {
			_, err := loadbalancer.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
			g.Expect(err).Should(MatchError("failed to get network my-network: Network.networking.api.onmetal.de \"my-network\" not found"))
		}).Should(Succeed())

		By("creating a network")
		network := &networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      networkName,
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())

		By("failing if no loadbalancer routing is present")
		Eventually(func(g Gomega) {
			err := loadbalancer.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node2})
			g.Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		}).Should(Succeed())

		lbName := getLoadBalancerName(clusterName, service)

		By("patching public IP into loadbalancer status")
		lb := &networkingv1alpha1.LoadBalancer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: lbName}, lb)).To(Succeed())
		lbBase := lb.DeepCopy()
		lb.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{{Addr: netip.AddrFrom4([4]byte{10, 0, 0, 1})}},
		}
		Expect(k8sClient.Status().Patch(ctx, lb, client.MergeFrom(lbBase))).To(Succeed())

		By("creating LoadBalancer for service")
		Eventually(func(g Gomega) {
			lbStatus, err := loadbalancer.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
			}))
		}).Should(Succeed())

		items := []commonv1alpha1.LocalUIDReference{}
		nic1 := &networkingv1alpha1.NetworkInterface{}
		nic2 := &networkingv1alpha1.NetworkInterface{}
		nic3 := &networkingv1alpha1.NetworkInterface{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: netInterface.Name}, nic1)).To(Succeed())
		if nic1.Spec.NetworkRef.Name == networkName {
			item := commonv1alpha1.LocalUIDReference{
				Name: nic1.Name,
				UID:  nic1.UID,
			}
			items = append(items, item)
		}

		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: netInterface2.Name}, nic2)).To(Succeed())
		if nic2.Spec.NetworkRef.Name == networkName {
			item := commonv1alpha1.LocalUIDReference{
				Name: nic2.Name,
				UID:  nic2.UID,
			}
			items = append(items, item)
		}

		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: netInterface1.Name}, nic3)).To(Succeed())
		if nic3.Spec.NetworkRef.Name == networkName {
			item := commonv1alpha1.LocalUIDReference{
				Name: nic3.Name,
				UID:  nic3.UID,
			}
			items = append(items, item)
		}

		By("ensuring that the destination addresses are updated")
		Eventually(func(g Gomega) {
			err := loadbalancer.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1, node2})
			g.Expect(err).NotTo(HaveOccurred())
			lbRouting := &networkingv1alpha1.LoadBalancerRouting{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: string(lbName)}, lbRouting)).To(Succeed())
			By("getting lbRouting ")
			g.Expect(items).To(Equal(lbRouting.Destinations))
		}).Should(Succeed())

		By("ensuring that the destination addresses are deleted")
		items = items[1:]
		Eventually(func(g Gomega) {
			err := loadbalancer.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node2})
			g.Expect(err).NotTo(HaveOccurred())
			lbRouting := &networkingv1alpha1.LoadBalancerRouting{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: string(lbName)}, lbRouting)).To(Succeed())
			By("getting lbRouting ")
			g.Expect(items).To(Equal(lbRouting.Destinations))
		}).Should(Succeed())

	})

	It("Should get Loadbalancer info", func() {
		By("creating a network")
		network := &networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "network-",
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())

		const uid types.UID = "2020de0-e47ef-wef4fr-6ytt54-ee7763d"

		By("creating a LoadBalancer")
		lbName := "envtest-" + "myservice-" + strings.Split(string(uid), "-")[0]
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: lbName,
				Name:         lbName,
			},
			Spec: networkingv1alpha1.LoadBalancerSpec{
				Type:       networkingv1alpha1.LoadBalancerTypePublic,
				IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				Ports: []networkingv1alpha1.LoadBalancerPort{
					{
						Port:    1,
						EndPort: pointer.Int32(3),
					},
				},
			},
			Status: networkingv1alpha1.LoadBalancerStatus{IPs: []commonv1alpha1.IP{{Addr: netip.MustParseAddr("10.0.0.1")}}},
		}

		Expect(k8sClient.Create(ctx, loadBalancer)).To(Succeed())
		By("getting the loadbalancer interface")
		lb, ok := provider.LoadBalancer()
		Expect(ok).To(BeTrue())

		service := &corev1.Service{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myservice",
				Namespace: ns.Name,
				UID:       uid,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
			Status: corev1.ServiceStatus{},
		}
		service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{IP: "10.0.0.1"},
		}
		By("ensuring that GetLoadBalancer returns loadbalancer status")
		Eventually(func(g Gomega) {
			status, exist, err := lb.GetLoadBalancer(ctx, "envtest", service)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(exist).To(BeTrue())
			ingresses := status.Ingress
			for _, ingress := range ingresses {
				g.Expect(ingress.IP).To(Equal("10.0.0.1"))
			}
		}).Should(Succeed())

		By("ensuring that LoadBalancer returns instance not found for non existing object")
		Eventually(func(g Gomega) {
			_, exist, err := lb.GetLoadBalancer(ctx, "envnon", service)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(Equal(cloudprovider.InstanceNotFound))
			g.Expect(exist).To(BeFalse())
		}).Should(Succeed())

	})

	It("Should delete Loadbalancer", func() {
		By("creating a network")
		network := &networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "network-",
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())

		const uid types.UID = "2020de0-e47ef-wef4fr-6ytt54-ee7763d"

		By("creating a LoadBalancer")
		lbName := "envtest-" + "myservice-" + strings.Split(string(uid), "-")[0]
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      lbName,
			},
			Spec: networkingv1alpha1.LoadBalancerSpec{
				Type:       networkingv1alpha1.LoadBalancerTypePublic,
				IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
				NetworkRef: corev1.LocalObjectReference{Name: network.Name},
				Ports: []networkingv1alpha1.LoadBalancerPort{
					{
						Port:    1,
						EndPort: pointer.Int32(3),
					},
				},
			},
			Status: networkingv1alpha1.LoadBalancerStatus{IPs: []commonv1alpha1.IP{{Addr: netip.MustParseAddr("10.0.0.1")}}},
		}

		Expect(k8sClient.Create(ctx, loadBalancer)).To(Succeed())
		By("getting the loadbalancer interface")
		lb, ok := provider.LoadBalancer()
		Expect(ok).To(BeTrue())

		service := &corev1.Service{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myservice",
				Namespace: ns.Name,
				UID:       uid,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
			Status: corev1.ServiceStatus{},
		}

		By("ensuring that LoadBalancer instance is deleted")
		Eventually(func(g Gomega) {
			err := lb.EnsureLoadBalancerDeleted(ctx, "envtest", service)
			g.Expect(err).NotTo(HaveOccurred())
		}).Should(Succeed())

		By("ensuring that LoadBalancer deletion returns instance not found for deleted object")
		Eventually(func(g Gomega) {
			err := lb.EnsureLoadBalancerDeleted(ctx, "envtest", service)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err).To(Equal(cloudprovider.InstanceNotFound))
		}).Should(Succeed())

	})
})
