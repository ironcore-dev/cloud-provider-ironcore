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
	"context"
	"time"

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
)

var _ = Describe("LoadBalancer", func() {
	const clusterName = "test"

	var (
		service      *corev1.Service
		node         *corev1.Node
		netInterface *networkingv1alpha1.NetworkInterface
	)

	ns, olb, network := SetupTest()

	BeforeEach(func(ctx SpecContext) {
		By("creating a network interface for machine1")
		netInterface = newNetworkInterface(ns.Name, "machine-networkinterface", network.Name, "10.0.0.5")
		Expect(k8sClient.Create(ctx, netInterface)).To(Succeed())

		By("creating machine")
		networkInterfaces := []computev1alpha1.NetworkInterface{
			{
				Name: "networkinterface",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: netInterface.Name,
					},
				},
			},
		}
		machine := newMachine(ns.Name, "machine", networkInterfaces)
		Expect(k8sClient.Create(ctx, machine)).To(Succeed())

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

		By("creating test service of type LoadBalancer")
		service = &corev1.Service{
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
	})

	It("should ensure external LoadBalancer", func(ctx SpecContext) {
		By("failing if no public IP is present for LoadBalancer")
		ensureCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		Expect(olb.EnsureLoadBalancer(ensureCtx, clusterName, service, []*corev1.Node{node})).Error().To(HaveOccurred())

		By("getting the LoadBalancer")
		loadBalancer := &networkingv1alpha1.LoadBalancer{}
		loadBalancerKey := client.ObjectKey{Namespace: ns.Name, Name: olb.GetLoadBalancerName(ctx, clusterName, service)}
		Expect(k8sClient.Get(ctx, loadBalancerKey, loadBalancer)).To(Succeed())

		By("inspecting the LoadBalancer")
		Expect(loadBalancer.Spec.Type).To(Equal(networkingv1alpha1.LoadBalancerTypePublic))

		By("patching public IP into LoadBalancer status")
		Eventually(UpdateStatus(loadBalancer, func() {
			loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
		})).Should(Succeed())

		By("ensuring LoadBalancer for service")
		Expect(olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).
			To(Equal(&corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
			}))

		By("waiting for the LoadBalancer object to report the IPs")
		Eventually(Object(loadBalancer)).Should(HaveField("Status.IPs", []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}))
	})

	It("should ensure internal LoadBalancer", func(ctx SpecContext) {
		By("creating test service of type internal LoadBalancer")
		internalService := &corev1.Service{
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
		Expect(k8sClient.Create(ctx, internalService)).To(Succeed())

		By("failing if no public IP is present for LoadBalancer")
		ensureCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		Expect(olb.EnsureLoadBalancer(ensureCtx, clusterName, internalService, []*corev1.Node{node})).Error().To(HaveOccurred())

		By("getting the LoadBalancer")
		loadBalancer := &networkingv1alpha1.LoadBalancer{}
		loadBalancerKey := client.ObjectKey{Namespace: ns.Name, Name: olb.GetLoadBalancerName(ctx, clusterName, internalService)}
		Expect(k8sClient.Get(ctx, loadBalancerKey, loadBalancer)).To(Succeed())

		By("inspecting the LoadBalancer")
		Expect(loadBalancer.Spec.Type).To(Equal(networkingv1alpha1.LoadBalancerTypeInternal))

		By("patching internal IP into LoadBalancer status")
		Eventually(UpdateStatus(loadBalancer, func() {
			loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")}
		})).Should(Succeed())

		By("ensuring LoadBalancer for service")
		Expect(olb.EnsureLoadBalancer(ctx, clusterName, internalService, []*corev1.Node{node})).
			To(Equal(&corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "100.0.0.1"}},
			}))

		By("removing internal LoadBalancer annotation from service")
		Eventually(Update(internalService, func() {
			internalService.Annotations = map[string]string{}
		})).Should(Succeed())

		By("patching public IP into LoadBalancer status")
		Eventually(UpdateStatus(loadBalancer, func() {
			loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
		})).Should(Succeed())

		By("ensuring LoadBalancer for service")
		Expect(olb.EnsureLoadBalancer(ctx, clusterName, internalService, []*corev1.Node{node})).Error().NotTo(HaveOccurred())

		By("ensuring that the LoadBalancer is of type public")
		Expect(k8sClient.Get(ctx, loadBalancerKey, loadBalancer)).To(Succeed())
		Expect(loadBalancer.Spec.Type).To(Equal(networkingv1alpha1.LoadBalancerTypePublic))

		By("ensuring LoadBalancerStatus for service has the correct public IP")
		Expect(olb.EnsureLoadBalancer(ctx, clusterName, internalService, []*corev1.Node{node})).
			To(Equal(&corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
			}))
	})

	It("should update LoadBalancer", func(ctx SpecContext) {
		By("creating a network interface for machine-2")
		networkInterface2 := newNetworkInterface(ns.Name, "machine-2-networkinterface1", network.Name, "10.0.0.1")
		Expect(k8sClient.Create(ctx, networkInterface2)).To(Succeed())

		By("creating a network interface for machine-2 in a different network")
		networkInterfaceWithWrongNetwork := newNetworkInterface(ns.Name, "machine-2-networkinterface2", "foo", "20.0.0.2")
		Expect(k8sClient.Create(ctx, networkInterfaceWithWrongNetwork)).To(Succeed())

		By("creating NetworkInterfaces for Machine 2")
		networkInterfaces := []computev1alpha1.NetworkInterface{
			{
				Name: "networkinterface1",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: networkInterface2.Name,
					},
				},
			},
			{
				Name: "networkinterface2",
				NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
					NetworkInterfaceRef: &corev1.LocalObjectReference{
						Name: networkInterfaceWithWrongNetwork.Name,
					},
				},
			},
		}
		machine2 := newMachine(ns.Name, "machine-2", networkInterfaces)
		Expect(k8sClient.Create(ctx, machine2)).To(Succeed())

		By("creating node2 object with a provider ID referencing the machine2")
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

		By("failing if no public IP is present for LoadBalancer")
		ensureCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		Expect(olb.EnsureLoadBalancer(ensureCtx, clusterName, service, []*corev1.Node{node})).Error().To(HaveOccurred())

		By("getting the LoadBalancer")
		loadBalancer := &networkingv1alpha1.LoadBalancer{}
		loadBalancerKey := client.ObjectKey{Namespace: ns.Name, Name: olb.GetLoadBalancerName(ctx, clusterName, service)}
		Expect(k8sClient.Get(ctx, loadBalancerKey, loadBalancer)).To(Succeed())

		By("inspecting the LoadBalancer")
		Expect(loadBalancer.Spec.Type).To(Equal(networkingv1alpha1.LoadBalancerTypePublic))

		By("patching public IP into LoadBalancer status")
		Eventually(UpdateStatus(loadBalancer, func() {
			loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("100.0.0.1")}
		})).Should(Succeed())

		By("creating LoadBalancer for service")
		lbStatus, err := olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})
		Expect(err).NotTo(HaveOccurred())
		Expect(lbStatus).To(Equal(&corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "100.0.0.1"}},
		}))

		By("ensuring destinations of load balancer routing gets updated for node and node2")
		err = olb.UpdateLoadBalancer(ctx, clusterName, service, []*corev1.Node{node, node2})
		Expect(err).NotTo(HaveOccurred())
		lbRouting := &networkingv1alpha1.LoadBalancerRouting{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: service.Namespace,
				Name:      loadBalancer.Name,
			},
		}
		Eventually(Object(lbRouting)).Should(SatisfyAll(
			HaveField("ObjectMeta.OwnerReferences", ContainElement(metav1.OwnerReference{
				APIVersion: "networking.api.onmetal.de/v1alpha1",
				Kind:       "LoadBalancer",
				Name:       loadBalancer.Name,
				UID:        loadBalancer.UID,
			})),
			// netInterface2 will not be listed in Destinations, because network "foo" used by netInterface2 does not exist
			HaveField("Destinations", ContainElements([]commonv1alpha1.LocalUIDReference{
				{
					Name: netInterface.Name,
					UID:  netInterface.UID,
				},
				{
					Name: networkInterface2.Name,
					UID:  networkInterface2.UID,
				}}))))
	})

	It("should get LoadBalancer info", func(ctx SpecContext) {
		By("ensuring that GetLoadBalancer returns instance not found for non existing object")
		_, exist, err := olb.GetLoadBalancer(ctx, "foo", service)
		Expect(err).To(HaveOccurred())
		Expect(exist).To(BeFalse())
	})

	It("should delete LoadBalancer", func(ctx SpecContext) {
		By("failing if no public IP is present for LoadBalancer")
		ensureCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		Expect(olb.EnsureLoadBalancer(ensureCtx, clusterName, service, []*corev1.Node{node})).Error().To(HaveOccurred())

		By("getting the LoadBalancer")
		loadBalancer := &networkingv1alpha1.LoadBalancer{}
		loadBalancerKey := client.ObjectKey{Namespace: ns.Name, Name: olb.GetLoadBalancerName(ctx, clusterName, service)}
		Expect(k8sClient.Get(ctx, loadBalancerKey, loadBalancer)).To(Succeed())

		By("inspecting the LoadBalancer")
		Expect(loadBalancer.Spec.Type).To(Equal(networkingv1alpha1.LoadBalancerTypePublic))

		By("patching public IP into LoadBalancer status")
		Eventually(UpdateStatus(loadBalancer, func() {
			loadBalancer.Status.IPs = []commonv1alpha1.IP{commonv1alpha1.MustParseIP("10.0.0.1")}
		})).Should(Succeed())

		By("ensuring LoadBalancer for service")
		Expect(olb.EnsureLoadBalancer(ctx, clusterName, service, []*corev1.Node{node})).
			To(Equal(&corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
			}))

		By("deleting the LoadBalancer")
		Expect(olb.EnsureLoadBalancerDeleted(ctx, clusterName, service)).Error().To(Not(HaveOccurred()))
	})
})

func newNetworkInterface(namespace string, name string, networkName string, ip string) *networkingv1alpha1.NetworkInterface {
	return &networkingv1alpha1.NetworkInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1alpha1.NetworkInterfaceSpec{
			NetworkRef: corev1.LocalObjectReference{Name: networkName},
			IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP(ip)}},
		},
	}
}

func newMachine(namespace, name string, networkInterfaces []computev1alpha1.NetworkInterface) *computev1alpha1.Machine {
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
