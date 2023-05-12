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
		_, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{})
		Expect(err).Should(MatchError(fmt.Sprintf("there are no available nodes for LoadBalancer service %s", client.ObjectKeyFromObject(service))))

		By("creating a Node object")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, node)

		By("failing if no machine is present for a given node object")
		status, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})
		Expect(status).To(BeNil())
		Expect(err).To(HaveOccurred())

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
		lb := &networkingv1alpha1.LoadBalancer{ObjectMeta: metav1.ObjectMeta{Name: lbName, Namespace: service.Namespace}}
		lbBase := lb.DeepCopy()
		lb.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, lb, client.MergeFrom(lbBase))).To(Succeed())

		By("creating LoadBalancer for service")
		lbStatus, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
		Expect(err).NotTo(HaveOccurred())
		Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
		}))

		Eventually(Object(lb)).Should(SatisfyAll(
			HaveField("Status.IPs", []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")})))

		nic1 := &networkingv1alpha1.NetworkInterface{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: netInterface.Name}}
		Eventually(Object(nic1)).Should(SatisfyAll(
			HaveField("ObjectMeta.Namespace", ns.Name),
			HaveField("ObjectMeta.Name", netInterface.Name),
		))

		nic2 := &networkingv1alpha1.NetworkInterface{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: netInterface2.Name}}
		Eventually(Object(nic2)).Should(SatisfyAll(
			HaveField("ObjectMeta.Namespace", ns.Name),
			HaveField("ObjectMeta.Name", netInterface2.Name),
		))

		nic3 := &networkingv1alpha1.NetworkInterface{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: netInterface1.Name}}
		Eventually(Object(nic3)).Should(SatisfyAll(
			HaveField("ObjectMeta.Namespace", ns.Name),
			HaveField("ObjectMeta.Name", netInterface1.Name),
		))

		By("ensuring that the load balancer routing is matching the destinations")
		err = olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1, node2})
		Expect(err).NotTo(HaveOccurred())
		lbRouting := &networkingv1alpha1.LoadBalancerRouting{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: lbName}}
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("ObjectMeta.OwnerReferences", ContainElement(metav1.OwnerReference{
				APIVersion: "networking.api.onmetal.de/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       lbName,
				UID:        lb.UID,
			})),
			HaveField("Destinations", ContainElements([]commonv1alpha1.LocalUIDReference{
				{
					Name: nic1.Name,
					UID:  nic1.UID,
				},
				{
					Name: nic3.Name,
					UID:  nic3.UID,
				}}))))

		By("ensuring that the destination addresses are deleted")
		err = olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node2})
		Expect(err).NotTo(HaveOccurred())
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("Destinations", ContainElements(
				[]commonv1alpha1.LocalUIDReference{{
					Name: nic3.Name,
					UID:  nic3.UID,
				}}))))

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

		lbName := olb.GetLoadBalancerName(ctx, clusterName, service)
		lb := &networkingv1alpha1.LoadBalancer{ObjectMeta: metav1.ObjectMeta{Name: lbName, Namespace: service.Namespace}}
		lbBase := lb.DeepCopy()
		lb.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, lb, client.MergeFrom(lbBase))).To(Succeed())

		By("ensuring that GetLoadBalancer returns the correct load-balancer status")
		status, exist, err := olb.GetLoadBalancer(ctx, clusterName, service)
		Expect(err).NotTo(HaveOccurred())
		Expect(exist).To(BeTrue())
		Expect(status.Ingress).To(ContainElements(service.Status.LoadBalancer.Ingress))

		By("ensuring that LoadBalancer returns instance not found for non existing object")
		_, exist, err = olb.GetLoadBalancer(ctx, "envnon", service)
		Expect(err).To(HaveOccurred())
		Expect(exist).To(BeFalse())

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

		lbName := olb.GetLoadBalancerName(ctx, clusterName, service)
		lb := &networkingv1alpha1.LoadBalancer{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: lbName}}

		Eventually(Object(lb)).Should(SatisfyAll(
			HaveField("ObjectMeta.Name", lbName),
			HaveField("ObjectMeta.Namespace", ns.Name)))

		By("ensuring that LoadBalancer instance is deleted successfully")
		err := olb.EnsureLoadBalancerDeleted(ctx, "test", service)
		Expect(err).NotTo(HaveOccurred())
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
