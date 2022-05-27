package onmetal

import (
	"context"
	"strings"

	"github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

type onmetalInstances struct {
	clientSet *onmetalapi.Clientset
}

func newOnmetalInstances(clientSet *onmetalapi.Clientset) cloudprovider.Instances {
	return &onmetalInstances{
		clientSet: clientSet,
	}
}

func (o *onmetalInstances) NodeAddresses(ctx context.Context, name types.NodeName) ([]corev1.NodeAddress, error) {
	// TODO understand how to extract namespace for the clientset (maybe get from default kubeconfig context)
	machine, err := o.clientSet.ComputeV1alpha1().Machines("").Get(ctx, string(name), metav1.GetOptions{})
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
	return addresses, nil
}

func (o *onmetalInstances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]corev1.NodeAddress, error) {
	return o.NodeAddresses(ctx, types.NodeName(nodeNameFromProviderID(providerID)))
}

func (o *onmetalInstances) InstanceID(_ context.Context, nodeName types.NodeName) (string, error) {
	return string(nodeName), nil
}

func (o *onmetalInstances) InstanceType(ctx context.Context, nodeName types.NodeName) (string, error) {
	machine, err := o.clientSet.ComputeV1alpha1().Machines("").Get(ctx, string(nodeName), metav1.GetOptions{})
	if err != nil {
		return "", errors.Wrap(err, "unable to get machine")
	}
	return machine.Spec.MachineClassRef.Name, nil
}

func (o *onmetalInstances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	return o.InstanceType(ctx, types.NodeName(nodeNameFromProviderID(providerID)))
}

func (o *onmetalInstances) AddSSHKeyToAllInstances(_ context.Context, _ string, _ []byte) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalInstances) CurrentNodeName(_ context.Context, hostName string) (types.NodeName, error) {
	return types.NodeName(hostName), nil
}

func (o *onmetalInstances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	nodeName := nodeNameFromProviderID(providerID)
	_, err := o.clientSet.ComputeV1alpha1().Machines("").Get(ctx, nodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "unable to get machine")
	}
	return true, nil
}

func (o *onmetalInstances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	nodeName := nodeNameFromProviderID(providerID)
	machine, err := o.clientSet.ComputeV1alpha1().Machines("").Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return false, errors.Wrap(err, "unable to get machine")
	}

	return machine.Status.State == v1alpha1.MachineStateShutdown, nil
}

func nodeNameFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, CDefaultCloudProviderName+"://")
}
