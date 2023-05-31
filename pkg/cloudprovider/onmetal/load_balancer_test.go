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

var _ = Describe("LoadBalancer", func() {

	const (
		clusterName = "test-cluster"
		serviceName = "test-service"
	)
	var (
		service      *corev1.Service
		node1        *corev1.Node
		netInterface *networkingv1alpha1.NetworkInterface
		lbName       string
		loadBalancer *networkingv1alpha1.LoadBalancer
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

		By("creating a network interface for machine1")
		netInterface = newNetwrokInterface(ns.Name, "machine1-netinterface", networkName, "10.0.0.5")
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, netInterface)

		By("creating machine1")
		networkInterfaces := []computev1alpha1.NetworkInterface{
			{
				Name: "netinterface",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: netInterface.Name,
					},
				},
			},
		}
		machine1 := newMachine(ns.Name, "machine1", networkName, networkInterfaces)
		Expect(k8sClient.Create(ctx, machine1)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, machine1)

		By("creating node1 object with a provider ID referencing the machine1")
		node1 = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine1.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node1)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, node1)

		By("creating test service of type LoadBalancer")
		service = &corev1.Service{
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

		lbName = olb.GetLoadBalancerName(ctx, clusterName, service)

		By("creating test LoadBalancer object")
		loadBalancer = &networkingv1alpha1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      lbName,
			},
			Spec: networkingv1alpha1.LoadBalancerSpec{
				Type:       networkingv1alpha1.LoadBalancerTypePublic,
				IPFamilies: service.Spec.IPFamilies,
				NetworkRef: corev1.LocalObjectReference{Name: networkName},
			},
		}
		Expect(k8sClient.Create(ctx, loadBalancer)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, loadBalancer)
	})

	It("should ensure external LoadBalancer", func() {

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

		By("failing if no public IP is present for LoadBalancer")
		status, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})
		Expect(status).To(BeNil())
		Expect(err).To(HaveOccurred())

		By("patching public IP into loadbalancer status")
		lbBase := loadBalancer.DeepCopy()
		loadBalancer.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, loadBalancer, client.MergeFrom(lbBase))).To(Succeed())

		By("creating LoadBalancer for service")
		lbStatus, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
		Expect(err).NotTo(HaveOccurred())
		Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
		}))

		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypePublic)),
			HaveField("Status.IPs", []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}),
		))
	})

	It("should ensure internal LoadBalancer", func() {
		By("adding internal LoadBalancer annotation to service")
		svcBase := service.DeepCopy()
		service.Annotations = map[string]string{
			InternalLoadBalancerAnnotation: "true",
		}
		Expect(k8sClient.Status().Patch(ctx, service, client.MergeFrom(svcBase))).To(Succeed())
		Eventually(Object(service)).Should(SatisfyAll(
			HaveField("Annotations", HaveKeyWithValue(InternalLoadBalancerAnnotation, "true")),
		))

		By("patching internal IP into loadbalancer status")
		lbBase := loadBalancer.DeepCopy()
		loadBalancer.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.2")},
		}
		Expect(k8sClient.Status().Patch(ctx, loadBalancer, client.MergeFrom(lbBase))).To(Succeed())

		By("ensuring internal LoadBalancer for service")
		lbStatus, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
		Expect(err).NotTo(HaveOccurred())
		Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.2"}},
		}))
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypeInternal)),
			HaveField("Status.IPs", []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.2")}),
		))

		By("removing internal LoadBalancer annotation from service")
		svcBase = service.DeepCopy()
		delete(service.Annotations, InternalLoadBalancerAnnotation)
		Expect(k8sClient.Status().Patch(ctx, service, client.MergeFrom(svcBase))).To(Succeed())
		Eventually(Object(service)).Should(SatisfyAll(
			HaveField("Annotations", BeEmpty()),
		))

		By("patching public IP into loadbalancer status")
		lbBase = loadBalancer.DeepCopy()
		loadBalancer.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, loadBalancer, client.MergeFrom(lbBase))).To(Succeed())

		By("ensuring external LoadBalancer for service after removing internal LoadBalancer annotation")
		lbStatus, err = olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
		Expect(err).NotTo(HaveOccurred())
		Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
		}))
		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("Spec.Type", Equal(networkingv1alpha1.LoadBalancerTypePublic)),
			HaveField("Status.IPs", []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}),
		))
	})

	It("should update LoadBalancer", func() {

		By("creating a network interface1 for machine 2")
		netInterface1 := newNetwrokInterface(ns.Name, "machine2-netinterface1", networkName, "10.0.0.5")
		Expect(k8sClient.Create(ctx, netInterface1)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, netInterface1)

		By("creating a network interface2 for machine 2")
		netInterface2 := newNetwrokInterface(ns.Name, "machine2-netinterface2", "my-network-2", "10.0.0.6")
		Expect(k8sClient.Create(ctx, netInterface2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, netInterface2)

		By("creating machine2")
		networkInterfaces := []computev1alpha1.NetworkInterface{
			{
				Name: "netinterface1",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: netInterface1.Name,
					},
				},
			},
			{
				Name: "netinterface2",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: netInterface2.Name,
					},
				},
			},
		}
		machine2 := newMachine(ns.Name, "machine2", networkName, networkInterfaces)
		Expect(k8sClient.Create(ctx, machine2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, machine2)

		By("creating node2 object with a provider ID referencing the machine2")
		node2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: machine2.Name,
			},
		}
		Expect(k8sClient.Create(ctx, node2)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, node2)

		By("patching public IP into loadbalancer status")
		lbBase := loadBalancer.DeepCopy()
		loadBalancer.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, loadBalancer, client.MergeFrom(lbBase))).To(Succeed())

		By("creating LoadBalancer for service")
		lbStatus, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1})
		Expect(err).NotTo(HaveOccurred())
		Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
		}))

		Eventually(Object(netInterface)).Should(SatisfyAll(
			HaveField("ObjectMeta.Namespace", ns.Name),
			HaveField("ObjectMeta.Name", netInterface.Name),
		))

		Eventually(Object(netInterface1)).Should(SatisfyAll(
			HaveField("ObjectMeta.Namespace", ns.Name),
			HaveField("ObjectMeta.Name", netInterface1.Name),
		))

		Eventually(Object(netInterface2)).Should(SatisfyAll(
			HaveField("ObjectMeta.Namespace", ns.Name),
			HaveField("ObjectMeta.Name", netInterface2.Name),
		))

		By("ensuring destinations of load balancer routing gets updated for node1 and node2")
		err = olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node1, node2})
		Expect(err).NotTo(HaveOccurred())
		lbRouting := &networkingv1alpha1.LoadBalancerRouting{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: lbName}}
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("ObjectMeta.OwnerReferences", ContainElement(metav1.OwnerReference{
				APIVersion: "networking.api.onmetal.de/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       lbName,
				UID:        loadBalancer.UID,
			})),
			// netInterface2 will not be listed in Destinations, because network "my-network-2" used by netInterface2 does not exist
			HaveField("Destinations", ContainElements([]commonv1alpha1.LocalUIDReference{
				{
					Name: netInterface.Name,
					UID:  netInterface.UID,
				},
				{
					Name: netInterface1.Name,
					UID:  netInterface1.UID,
				}}))))

		By("ensuring destinations of load balancer routing gets updated for only node2")
		err = olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node2})
		Expect(err).NotTo(HaveOccurred())
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("Destinations", ContainElements(
				[]commonv1alpha1.LocalUIDReference{{
					Name: netInterface1.Name,
					UID:  netInterface1.UID,
				}}))))
	})

	It("should get LoadBalancer info", func() {

		service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{IP: "10.0.0.1"},
		}

		lbBase := loadBalancer.DeepCopy()
		loadBalancer.Status = networkingv1alpha1.LoadBalancerStatus{
			IPs: []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")},
		}
		Expect(k8sClient.Status().Patch(ctx, loadBalancer, client.MergeFrom(lbBase))).To(Succeed())

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

	It("should delete LoadBalancer", func() {

		Eventually(Object(loadBalancer)).Should(SatisfyAll(
			HaveField("ObjectMeta.Name", lbName),
			HaveField("ObjectMeta.Namespace", ns.Name)))

		By("ensuring that LoadBalancer instance is deleted successfully")
		err := olb.EnsureLoadBalancerDeleted(ctx, "test", service)
		Expect(err).NotTo(HaveOccurred())
	})
})

func newNetwrokInterface(namespace string, name string, networkName string, ip string) *networkingv1alpha1.NetworkInterface {
	return &networkingv1alpha1.NetworkInterface{
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
}

func newMachine(namespace, name, networkName string, networkInterfaces []computev1alpha1.NetworkInterface) *computev1alpha1.Machine {
	return &computev1alpha1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: computev1alpha1.MachineSpec{
			MachineClassRef:   corev1.LocalObjectReference{Name: "machine-class"},
			Image:             "my-image:latest",
			NetworkInterfaces: networkInterfaces,
			Volumes:           []computev1alpha1.Volume{},
		},
	}
}
