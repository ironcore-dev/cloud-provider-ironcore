package onmetal

import (
	"context"

	"github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalInstancesV2 struct {
	clientSet *onmetalapi.Clientset
	namespace string
}

func newOnmetalInstancesV2(clientSet *onmetalapi.Clientset, namespace string) cloudprovider.InstancesV2 {
	return &onmetalInstancesV2{
		clientSet: clientSet,
		namespace: namespace,
	}
}

func (o *onmetalInstancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	_, err := o.clientSet.ComputeV1alpha1().Machines(o.namespace).Get(ctx, node.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "unable to get machine")
	}
	return true, nil
}

func (o *onmetalInstancesV2) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	machine, err := o.clientSet.ComputeV1alpha1().Machines(o.namespace).Get(ctx, node.Name, metav1.GetOptions{})
	if err != nil {
		return false, errors.Wrap(err, "unable to get machine")
	}

	return machine.Status.State == v1alpha1.MachineStateShutdown, nil
}

func (o *onmetalInstancesV2) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	machine, err := o.clientSet.ComputeV1alpha1().Machines(o.namespace).Get(ctx, node.Name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to get machine")
	}

	addresses := make([]corev1.NodeAddress, 0)
	for _, iface := range machine.Status.NetworkInterfaces {
		virtualIP := iface.VirtualIP.String()
		// TODO understand how to differentiate addresses
		addresses = append(addresses, corev1.NodeAddress{
			Type:    corev1.NodeInternalIP,
			Address: virtualIP,
		})
		for _, ip := range iface.IPs {
			ip := ip.String()
			addresses = append(addresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: ip,
			})
		}
	}

	// TODO understand how to get zones and regions
	meta := &cloudprovider.InstanceMetadata{
		ProviderID:    node.Spec.ProviderID,
		InstanceType:  machine.Spec.MachineClassRef.Name,
		NodeAddresses: addresses,
		Zone:          "",
		Region:        "",
	}

	return meta, nil
}
