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

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	"github.com/onmetal/onmetal-api/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/utils/pointer"
)

var _ = Describe("LoadBalancer", func() {
	ctx := testutils.SetupContext()
	ns := SetupTest(ctx)

	It("Should get Loadbalancer info", func() {
		By("creating a network")
		network := &networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "network-",
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())

		const uid types.UID = "2020d-e0e-47ef-wef4fr-6ytt54-ee7763d"
		uidHash := getHashValueForUID([]byte(uid))

		By("creating a LoadBalancer")
		lbName := trimLoadBalancerName("envtest-" + "myservice-" + uidHash)
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

		const uid types.UID = "2020d-e0e-47ef-wef4fr-6ytt54-ee7763d"
		uidHash := getHashValueForUID([]byte(uid))

		By("creating a LoadBalancer")
		lbName := trimLoadBalancerName("envtest-" + "myservice-" + uidHash)
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
