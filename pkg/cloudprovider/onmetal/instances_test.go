package onmetal

import (
	"context"
	"time"

	commonv1alpha1 "github.com/onmetal/onmetal-api/apis/common/v1alpha1"
	computev1alpha1 "github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/apis/networking/v1alpha1"
	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Instances", func() {
	const (
		timeout  = time.Second * 5
		interval = time.Millisecond * 250

		Namespace = "default"
	)

	AfterEach(func() {
		ctx := context.Background()
		resources := []struct {
			res   client.Object
			list  client.ObjectList
			count func(client.ObjectList) int
		}{
			{
				res:  &computev1alpha1.Machine{},
				list: &computev1alpha1.MachineList{},
				count: func(objList client.ObjectList) int {
					list := objList.(*computev1alpha1.MachineList)
					return len(list.Items)
				},
			},
			{
				res:  &networkingv1alpha1.Network{},
				list: &networkingv1alpha1.NetworkList{},
				count: func(objList client.ObjectList) int {
					list := objList.(*networkingv1alpha1.NetworkList)
					return len(list.Items)
				},
			},
		}

		for _, r := range resources {
			Expect(k8sClient.DeleteAllOf(ctx, r.res, client.InNamespace(Namespace))).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.List(ctx, r.list)
				if err != nil {
					return false
				}
				if r.count(r.list) > 0 {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
		}
	})

	Context("Instances test", func() {
		It("Should get instance info", func() {
			ctx := context.Background()

			network := &networkingv1alpha1.Network{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    Namespace,
					GenerateName: "network-",
				},
			}
			Expect(k8sClient.Create(ctx, network)).To(Succeed())

			nodeIpString := "10.0.0.1"

			By("creating a machine")
			machine := &computev1alpha1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    Namespace,
					GenerateName: "machine-",
				},
				Spec: computev1alpha1.MachineSpec{
					MachineClassRef: corev1.LocalObjectReference{Name: "machine-class"},
					Image:           "my-image:latest",
					NetworkInterfaces: []computev1alpha1.NetworkInterface{
						{
							Name: "interface",
							NetworkInterfaceSource: computev1alpha1.NetworkInterfaceSource{
								Ephemeral: &computev1alpha1.EphemeralNetworkInterfaceSource{
									NetworkInterfaceTemplate: &networkingv1alpha1.NetworkInterfaceTemplateSpec{
										Spec: networkingv1alpha1.NetworkInterfaceSpec{
											NetworkRef: corev1.LocalObjectReference{Name: network.Name},
											IPs:        []networkingv1alpha1.IPSource{{Value: commonv1alpha1.MustParseNewIP(nodeIpString)}},
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

			onmetalClientSet, err := onmetalapi.NewForConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			instances := newOnmetalInstances(onmetalClientSet, Namespace)

			addresses, err := instances.NodeAddresses(ctx, types.NodeName(machine.Name))
			Expect(err).NotTo(HaveOccurred())
			Expect(addresses).To(HaveLen(1))
			Expect(addresses).To(ContainElements(corev1.NodeAddress{
				Type:    corev1.NodeHostName,
				Address: machine.Name,
			}))

			addresses, err = instances.NodeAddressesByProviderID(ctx, CDefaultCloudProviderName+"://"+machine.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(addresses).To(HaveLen(1))
			Expect(addresses).To(ContainElements(corev1.NodeAddress{
				Type:    corev1.NodeHostName,
				Address: machine.Name,
			}))

			nodeName, err := instances.InstanceID(ctx, types.NodeName(machine.Name))
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeName).To(Equal(machine.Name))

			instanceType, err := instances.InstanceType(ctx, types.NodeName(machine.Name))
			Expect(err).NotTo(HaveOccurred())
			Expect(instanceType).To(Equal(machine.Spec.MachineClassRef.Name))

			err = instances.AddSSHKeyToAllInstances(ctx, "", []byte{})
			Expect(err).To(Equal(cloudprovider.NotImplemented))

			nodeNameTyped, err := instances.CurrentNodeName(ctx, machine.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeNameTyped).To(Equal(types.NodeName(machine.Name)))

			exists, err := instances.InstanceExistsByProviderID(ctx, CDefaultCloudProviderName+"://"+machine.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())

			shutdown, err := instances.InstanceShutdownByProviderID(ctx, CDefaultCloudProviderName+"://"+machine.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(shutdown).To(BeFalse())
		})
	})
})
