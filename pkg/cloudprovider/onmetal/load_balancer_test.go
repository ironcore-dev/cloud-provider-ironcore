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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	"github.com/onmetal/onmetal-api/utils/testing"
)

type NetworkInterfaceArgs struct {
	Name        string
	NetworkName string
	IPs         []string
}

var _ = Describe("LoadBalancer", func() {

	const (
		clusterName = "test-cluster"
		serviceName = "test-service"
	)

	ctx := testing.SetupContext()
	ns, olb, networkName := SetupTest(ctx)

	BeforeEach(func() {
		By("creating test network")
		network := &networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      networkName,
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, network)

		By("creating test service of type LoadBalancer")
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
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
		DeferCleanup(k8sClient.Delete, ctx, service)

		By("creating test LoadBalancer object")
		lbName := olb.GetLoadBalancerName(ctx, clusterName, service)
		lbPorts := []networkingv1alpha1.LoadBalancerPort{}
		for _, svcPort := range service.Spec.Ports {
			lbPorts = append(lbPorts, networkingv1alpha1.LoadBalancerPort{
				Protocol: &svcPort.Protocol,
				Port:     svcPort.Port,
			})
		}
		loadBalancer := &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      lbName,
			},
			Spec: networkingv1alpha1.LoadBalancerSpec{
				Type:       networkingv1alpha1.LoadBalancerTypePublic,
				IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
				NetworkRef: corev1.LocalObjectReference{Name: networkName},
				Ports:      lbPorts,
			},
		}
		Expect(k8sClient.Create(ctx, loadBalancer)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, loadBalancer)
	})

	It("should ensure and update LoadBalancer info", func() {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: ns.Name,
			},
		}
		Eventually(Object(service)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(corev1.ServiceTypeLoadBalancer)),
			HaveField("Spec.Ports", ConsistOf(
				MatchFields(IgnoreMissing|IgnoreExtras, Fields{
					"Name":       Equal("https"),
					"Protocol":   Equal(corev1.Protocol("TCP")),
					"Port":       Equal(int32(443)),
					"TargetPort": Equal(intstr.IntOrString{IntVal: 443}),
					"NodePort":   Equal(int32(31376)),
				}),
			)),
		))

		By("expecting an error if there are no nodes available")
		Eventually(func(g Gomega) {
			_, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{})
			g.Expect(err).Should(MatchError(fmt.Sprintf("there are no available nodes for LoadBalancer service %s", client.ObjectKeyFromObject(service))))
		}).Should(Succeed())

		By("creating a Node object")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, node)

		By("failing if no machine is present for a given node object")
		Eventually(func(g Gomega) {
			status, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})
			g.Expect(status).To(BeNil())
			g.Expect(err).To(HaveOccurred())
		}).Should(Succeed())

		By("creating a network interface1 for machine 1")
		netInterface := newNetwrokInterface(ns.Name, "machine1-netinterface0", networkName, "10.0.0.5")
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, netInterface)

		By("creating a network interface2 for machine 1")
		netInterface2 := newNetwrokInterface(ns.Name, "machine1-netinterface1", "my-network-2", "10.0.0.6")
		Expect(k8sClient.Create(ctx, netInterface2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, netInterface2)

		By("creating a network interface1 for machine 2")
		netInterface1 := newNetwrokInterface(ns.Name, "machine2-netinterface2", networkName, "10.0.0.7")
		Expect(k8sClient.Create(ctx, netInterface1)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, netInterface1)

		By("creating machine1")
		machine1NetworkInterfaces := []NetworkInterfaceArgs{
			{
				Name: "netinterface0",
				IPs:  []string{"10.0.0.5"},
			},
			{
				Name: "netinterface1",
				IPs:  []string{"10.0.0.6"},
			},
		}
		machine1 := newMachine(ns.Name, "machine1", networkName, machine1NetworkInterfaces)
		Expect(k8sClient.Create(ctx, machine1)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, machine1)

		By("creating machine2")
		machine2NetworkInterfaces := []NetworkInterfaceArgs{
			{
				Name: "netinterface2",
				IPs:  []string{"10.0.0.7"},
			},
		}
		machine2 := newMachine(ns.Name, "machine2", networkName, machine2NetworkInterfaces)
		Expect(k8sClient.Create(ctx, machine2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, machine2)

		By("creating node1 object with a provider ID referencing the machine1")
		node1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine1.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node1)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, node1)

		By("creating node2 object with a provider ID referencing the machine2")
		node2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine2.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, node2)

		lbName := olb.GetLoadBalancerName(ctx, clusterName, service)

		By("patching public IP into loadbalancer status")
		lb := &networkingv1alpha1.LoadBalancer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: lbName}, lb)).To(Succeed())
		lbBase := lb.DeepCopy()
		lb.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, lb, client.MergeFrom(lbBase))).To(Succeed())

		By("creating LoadBalancer for service")
		Eventually(func(g Gomega) {
			lbStatus, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
			}))
		}).Should(Succeed())

		var items []commonv1alpha1.LocalUIDReference

		nic1 := &networkingv1alpha1.NetworkInterface{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: netInterface.Name}, nic1)).To(Succeed())
		if nic1.Spec.NetworkRef.Name == networkName {
			item := commonv1alpha1.LocalUIDReference{
				Name: nic1.Name,
				UID:  nic1.UID,
			}
			items = append(items, item)
		}

		nic2 := &networkingv1alpha1.NetworkInterface{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: netInterface2.Name}, nic2)).To(Succeed())
		if nic2.Spec.NetworkRef.Name == networkName {
			item := commonv1alpha1.LocalUIDReference{
				Name: nic2.Name,
				UID:  nic2.UID,
			}
			items = append(items, item)
		}

		nic3 := &networkingv1alpha1.NetworkInterface{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: netInterface1.Name}, nic3)).To(Succeed())
		if nic3.Spec.NetworkRef.Name == networkName {
			item := commonv1alpha1.LocalUIDReference{
				Name: nic3.Name,
				UID:  nic3.UID,
			}
			items = append(items, item)
		}

		By("ensuring that the load balancer routing is matching the destinations")
		Eventually(func(g Gomega) {
			err := olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1, node2})
			g.Expect(err).NotTo(HaveOccurred())
			lbRouting := &networkingv1alpha1.LoadBalancerRouting{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: lbName}, lbRouting)).To(Succeed())
			g.Expect(lbRouting.OwnerReferences).To(ContainElement(metav1.OwnerReference{
				APIVersion: "networking.api.onmetal.de/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       lbName,
				UID:        lb.UID,
			}))
			g.Expect(items).To(Equal(lbRouting.Destinations))
		}).Should(Succeed())

		By("ensuring that the destination addresses are deleted")
		items = items[1:]
		Eventually(func(g Gomega) {
			err := olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node2})
			g.Expect(err).NotTo(HaveOccurred())
			lbRouting := &networkingv1alpha1.LoadBalancerRouting{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: lbName}, lbRouting)).To(Succeed())
			g.Expect(items).To(Equal(lbRouting.Destinations))
		}).Should(Succeed())

	})

	It("should get Loadbalancer info", func() {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: ns.Name,
			},
		}
		Eventually(Object(service)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(corev1.ServiceTypeLoadBalancer)),
			HaveField("Spec.Ports", ConsistOf(
				MatchFields(IgnoreMissing|IgnoreExtras, Fields{
					"Name":       Equal("https"),
					"Protocol":   Equal(corev1.Protocol("TCP")),
					"Port":       Equal(int32(443)),
					"TargetPort": Equal(intstr.IntOrString{IntVal: 443}),
					"NodePort":   Equal(int32(31376)),
				}),
			)),
		))

		service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{IP: "10.0.0.1"},
		}

		By("ensuring that GetLoadBalancer returns the correct load-balancer status")
		Eventually(func(g Gomega) {
			status, exist, err := olb.GetLoadBalancer(ctx, clusterName, service)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(exist).To(BeTrue())
			ingresses := status.Ingress
			for _, ingress := range ingresses {
				g.Expect(ingress.IP).To(Equal(service.Status.LoadBalancer.Ingress[0].IP))
			}
		}).Should(Succeed())

		By("ensuring that LoadBalancer returns instance not found for non existing object")
		Eventually(func(g Gomega) {
			_, exist, err := olb.GetLoadBalancer(ctx, "envnon", service)
			g.Expect(err).To(HaveOccurred())
			g.Expect(exist).To(BeFalse())
		}).Should(Succeed())

	})

	It("should delete Loadbalancer", func() {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: ns.Name,
			},
		}
		Eventually(Object(service)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(corev1.ServiceTypeLoadBalancer)),
			HaveField("Spec.Ports", ConsistOf(
				MatchFields(IgnoreMissing|IgnoreExtras, Fields{
					"Name":       Equal("https"),
					"Protocol":   Equal(corev1.Protocol("TCP")),
					"Port":       Equal(int32(443)),
					"TargetPort": Equal(intstr.IntOrString{IntVal: 443}),
					"NodePort":   Equal(int32(31376)),
				}),
			)),
		))

		By("ensuring that LoadBalancer instance is deleted successfully")
		Eventually(func(g Gomega) {
			err := olb.EnsureLoadBalancerDeleted(ctx, "test", service)
			g.Expect(err).NotTo(HaveOccurred())
		}).Should(Succeed())

	})
})

func newNetwrokInterface(namespace string, name string, networkName string, ip string) *networkingv1alpha1.NetworkInterface {
	netInterface := &networkingv1alpha1.NetworkInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1alpha1.NetworkInterfaceSpec{
			NetworkRef: corev1.LocalObjectReference{Name: networkName},
			IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP(ip)}},
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

	return netInterface
}

func newMachine(namespace, name, networkName string, networkInterfaces []NetworkInterfaceArgs) *computev1alpha1.Machine {
	var netInterfaces []computev1alpha1.NetworkInterface
	for _, ni := range networkInterfaces {
		var ips []networkingv1alpha1.IPSource
		for _, ip := range ni.IPs {
			ips = append(ips, networkingv1alpha1.IPSource{Value: commonv1alpha1.MustParseNewIP(ip)})
		}
		netInterfaces = append(netInterfaces, computev1alpha1.NetworkInterface{
			Name: ni.Name,
			NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
				Ephemeral: &computev1alpha1.EphemeralNetworkInterfaceSource{
					NetworkInterfaceTemplate: &networkingv1alpha1.NetworkInterfaceTemplateSpec{
						Spec: networkingv1alpha1.NetworkInterfaceSpec{
							NetworkRef: corev1.LocalObjectReference{Name: networkName},
							IPs:        ips,
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
		})
	}

	machine := &computev1alpha1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: computev1alpha1.MachineSpec{
			MachineClassRef:   corev1.LocalObjectReference{Name: "machine-class"},
			Image:             "my-image:latest",
			NetworkInterfaces: netInterfaces,
			Volumes:           []computev1alpha1.Volume{},
		},
	}

	return machine
}
