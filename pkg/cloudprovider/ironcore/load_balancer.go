// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	servicehelper "k8s.io/cloud-provider/service/helpers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
)

const (
	waitLoadbalancerInitDelay   = 1 * time.Second
	waitLoadbalancerFactor      = 1.2
	waitLoadbalancerActiveSteps = 19
)

var (
	loadBalancerFieldOwner = client.FieldOwner("cloud-provider.ironcore.dev/loadbalancer")
)

type ironcoreLoadBalancer struct {
	targetClient      client.Client
	ironcoreClient    client.Client
	ironcoreNamespace string
	cloudConfig       CloudConfig
}

func newIroncoreLoadBalancer(targetClient client.Client, ironcoreClient client.Client, namespace string, cloudConfig CloudConfig) cloudprovider.LoadBalancer {
	return &ironcoreLoadBalancer{
		targetClient:      targetClient,
		ironcoreClient:    ironcoreClient,
		ironcoreNamespace: namespace,
		cloudConfig:       cloudConfig,
	}
}

func (o *ironcoreLoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	klog.V(2).InfoS("GetLoadBalancer for Service", "Cluster", clusterName, "Service", client.ObjectKeyFromObject(service))

	loadBalancerName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get LoadBalancer name for Service %s: %w", client.ObjectKeyFromObject(service), err)
	}
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	if err = o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}, loadBalancer); err != nil {
		return nil, false, fmt.Errorf("failed to get LoadBalancer %s for Service %s: %w", loadBalancerName, client.ObjectKeyFromObject(service), err)
	}

	lbAllocatedIps := loadBalancer.Status.IPs
	status = &v1.LoadBalancerStatus{}
	for _, ip := range lbAllocatedIps {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ip.String()})
	}
	return status, true, nil
}

func getLegacyLoadBalancerName(clusterName string, service *v1.Service) string {
	nameSuffix := strings.Split(string(service.UID), "-")[0]
	return fmt.Sprintf("%s-%s-%s", clusterName, service.Name, nameSuffix)
}

func (o *ironcoreLoadBalancer) getLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) (string, error) {
	legacyLoadBalancerName := getLegacyLoadBalancerName(clusterName, service)
	existingLoadBalancer := &networkingv1alpha1.LoadBalancer{}

	if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: legacyLoadBalancerName}, existingLoadBalancer); err != nil {
		if apierrors.IsNotFound(err) {
			return strings.Replace(string(service.UID), "-", "", -1), nil
		}
		return "", err
	}
	return legacyLoadBalancerName, nil
}

func (o *ironcoreLoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	lbName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		klog.V(2).ErrorS(err, "failed to get LoadBalancer for Service", "Service", client.ObjectKeyFromObject(service))
		return ""
	}
	return lbName
}

func (o *ironcoreLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	klog.V(2).InfoS("EnsureLoadBalancer for Service", "Cluster", clusterName, "Service", client.ObjectKeyFromObject(service))

	// decide load balancer type based on service annotation for internal load balancer
	var desiredLoadBalancerType networkingv1alpha1.LoadBalancerType
	if value, ok := service.Annotations[InternalLoadBalancerAnnotation]; ok && value == "true" {
		desiredLoadBalancerType = networkingv1alpha1.LoadBalancerTypeInternal
	} else {
		desiredLoadBalancerType = networkingv1alpha1.LoadBalancerTypePublic
	}

	loadBalancerName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		return nil, fmt.Errorf("failed to get LoadBalancer name for Service %s: %w", client.ObjectKeyFromObject(service), err)
	}

	// get existing load balancer type
	existingLoadBalancer := &networkingv1alpha1.LoadBalancer{}
	var existingLoadBalancerType networkingv1alpha1.LoadBalancerType
	if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}, existingLoadBalancer); err == nil {
		existingLoadBalancerType = existingLoadBalancer.Spec.Type
		if existingLoadBalancerType != desiredLoadBalancerType {
			if err = o.EnsureLoadBalancerDeleted(ctx, clusterName, service); err != nil {
				return nil, fmt.Errorf("failed deleting existing loadbalancer %s: %w", loadBalancerName, err)
			}
		}
	}

	klog.V(2).InfoS("Getting LoadBalancer ports from Service", "Service", client.ObjectKeyFromObject(service))
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
			Name:      loadBalancerName,
			Namespace: o.ironcoreNamespace,
			Annotations: map[string]string{
				AnnotationKeyClusterName:      clusterName,
				AnnotationKeyServiceName:      service.Name,
				AnnotationKeyServiceNamespace: service.Namespace,
				AnnotationKeyServiceUID:       string(service.UID),
			},
		},
		Spec: networkingv1alpha1.LoadBalancerSpec{
			Type:       desiredLoadBalancerType,
			IPFamilies: service.Spec.IPFamilies,
			NetworkRef: v1.LocalObjectReference{
				Name: o.cloudConfig.NetworkName,
			},
			Ports: lbPorts,
		},
	}

	// if load balancer type is Internal then update IPSource with valid prefix template
	if desiredLoadBalancerType == networkingv1alpha1.LoadBalancerTypeInternal {
		if o.cloudConfig.PrefixName == "" {
			return nil, fmt.Errorf("prefixName is not defined in config")
		}
		loadBalancer.Spec.IPs = []networkingv1alpha1.IPSource{
			{
				Ephemeral: &networkingv1alpha1.EphemeralPrefixSource{
					PrefixTemplate: &ipamv1alpha1.PrefixTemplateSpec{
						Spec: ipamv1alpha1.PrefixSpec{
							// TODO: for now we only support IPv4 until Gardener has support for IPv6 based Shoots
							IPFamily: v1.IPv4Protocol,
							ParentRef: &v1.LocalObjectReference{
								Name: o.cloudConfig.PrefixName,
							},
						},
					},
				},
			},
		}
	}

	klog.V(2).InfoS("Applying LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))
	if err := o.ironcoreClient.Patch(ctx, loadBalancer, client.Apply, loadBalancerFieldOwner, client.ForceOwnership); err != nil {
		return nil, fmt.Errorf("failed to apply LoadBalancer %s for Service %s: %w", client.ObjectKeyFromObject(loadBalancer), client.ObjectKeyFromObject(service), err)
	}
	klog.V(2).InfoS("Applied LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))

	klog.V(2).InfoS("Applying LoadBalancerRouting for LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	if err := o.applyLoadBalancerRoutingForLoadBalancer(ctx, loadBalancer, nodes); err != nil {
		return nil, err
	}
	klog.V(2).InfoS("Applied LoadBalancerRouting for LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))

	lbStatus, err := waitLoadBalancerActive(ctx, o.ironcoreClient, existingLoadBalancerType, service, loadBalancer)
	if err != nil {
		return nil, err
	}
	return &lbStatus, nil
}

func waitLoadBalancerActive(ctx context.Context, ironcoreClient client.Client, existingLoadBalancerType networkingv1alpha1.LoadBalancerType,
	service *v1.Service, loadBalancer *networkingv1alpha1.LoadBalancer) (v1.LoadBalancerStatus, error) {
	klog.V(2).InfoS("Waiting for LoadBalancer instance to become ready", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    waitLoadbalancerActiveSteps,
	}

	loadBalancerStatus := v1.LoadBalancerStatus{}
	if err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		if err := ironcoreClient.Get(ctx, client.ObjectKey{Namespace: loadBalancer.Namespace, Name: loadBalancer.Name}, loadBalancer); err != nil {
			return false, err
		}
		if len(loadBalancer.Status.IPs) == 0 {
			return false, nil
		}
		lbIngress := []v1.LoadBalancerIngress{}
		for _, ipAddr := range loadBalancer.Status.IPs {
			lbIngress = append(lbIngress, v1.LoadBalancerIngress{IP: ipAddr.String()})
		}
		loadBalancerStatus.Ingress = lbIngress

		if loadBalancer.Spec.Type != existingLoadBalancerType && servicehelper.LoadBalancerStatusEqual(&service.Status.LoadBalancer, &loadBalancerStatus) {
			return false, nil
		}
		return true, nil
	}); wait.Interrupted(err) {
		return loadBalancerStatus, fmt.Errorf("timeout waiting for the LoadBalancer %s to become ready", client.ObjectKeyFromObject(loadBalancer))
	}

	klog.V(2).InfoS("LoadBalancer became ready", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	return loadBalancerStatus, nil
}

func (o *ironcoreLoadBalancer) applyLoadBalancerRoutingForLoadBalancer(ctx context.Context, loadBalancer *networkingv1alpha1.LoadBalancer, nodes []*v1.Node) error {
	loadBalacerDestinations, err := o.getLoadBalancerDestinationsForNodes(ctx, nodes, loadBalancer.Spec.NetworkRef.Name)
	if err != nil {
		return fmt.Errorf("failed to get NetworkInterfaces for Nodes: %w", err)
	}

	network := &networkingv1alpha1.Network{}
	networkKey := client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancer.Spec.NetworkRef.Name}
	if err := o.ironcoreClient.Get(ctx, networkKey, network); err != nil {
		return fmt.Errorf("failed to get Network %s: %w", o.cloudConfig.NetworkName, err)
	}

	loadBalancerRouting := &networkingv1alpha1.LoadBalancerRouting{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LoadBalancerRouting",
			APIVersion: networkingv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancer.Name,
			Namespace: o.ironcoreNamespace,
		},
		NetworkRef: commonv1alpha1.LocalUIDReference{
			Name: network.Name,
			UID:  network.UID,
		},
		Destinations: loadBalacerDestinations,
	}

	if err := controllerutil.SetOwnerReference(loadBalancer, loadBalancerRouting, o.ironcoreClient.Scheme()); err != nil {
		return fmt.Errorf("failed to set owner reference for load balancer routing %s: %w", client.ObjectKeyFromObject(loadBalancerRouting), err)
	}

	if err := o.ironcoreClient.Patch(ctx, loadBalancerRouting, client.Apply, loadBalancerFieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancerRouting), client.ObjectKeyFromObject(loadBalancer), err)
	}
	return nil
}

func (o *ironcoreLoadBalancer) getLoadBalancerDestinationsForNodes(ctx context.Context, nodes []*v1.Node, networkName string) ([]networkingv1alpha1.LoadBalancerDestination, error) {
	var loadbalancerDestinations []networkingv1alpha1.LoadBalancerDestination
	for _, node := range nodes {
		machineName := extractMachineNameFromProviderID(node.Spec.ProviderID)
		machine := &computev1alpha1.Machine{}
		if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: machineName}, machine); client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("failed to get machine object for node %s: %w", node.Name, err)
		}

		for _, machineNIC := range machine.Spec.NetworkInterfaces {
			networkInterface := &networkingv1alpha1.NetworkInterface{}
			networkInterfaceName := fmt.Sprintf("%s-%s", machine.Name, machineNIC.Name)

			if machineNIC.NetworkInterfaceRef != nil {
				networkInterfaceName = machineNIC.NetworkInterfaceRef.Name
			}

			if err := o.ironcoreClient.Get(ctx, client.ObjectKey{Namespace: o.ironcoreNamespace, Name: networkInterfaceName}, networkInterface); err != nil {
				return nil, fmt.Errorf("failed to get network interface %s for machine %s: %w", client.ObjectKeyFromObject(networkInterface), client.ObjectKeyFromObject(machine), err)
			}

			// If the NetworkInterface is not part of Network we continue
			if networkInterface.Spec.NetworkRef.Name != networkName {
				continue
			}

			// Create a LoadBalancerDestination for every NetworkInterface IP
			for _, nicIP := range networkInterface.Status.IPs {
				loadbalancerDestinations = append(loadbalancerDestinations, networkingv1alpha1.LoadBalancerDestination{
					IP: nicIP,
					TargetRef: &networkingv1alpha1.LoadBalancerTargetRef{
						UID:        networkInterface.UID,
						Name:       networkInterface.Name,
						ProviderID: networkInterface.Spec.ProviderID,
					},
				})
			}
		}
	}
	return loadbalancerDestinations, nil
}

func extractMachineNameFromProviderID(providerID string) string {
	lastSlash := strings.LastIndex(providerID, "/")
	if lastSlash == -1 || lastSlash+1 >= len(providerID) {
		return ""
	}
	return providerID[lastSlash+1:]
}

func (o *ironcoreLoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	klog.V(2).InfoS("Updating LoadBalancer for Service", "Service", client.ObjectKeyFromObject(service))
	if len(nodes) == 0 {
		return fmt.Errorf("no Nodes available for LoadBalancer Service %s", client.ObjectKeyFromObject(service))
	}

	loadBalancerName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		return fmt.Errorf("failed to get LoadBalancer name for Service %s: %w", client.ObjectKeyFromObject(service), err)
	}

	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	loadBalancerKey := client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}
	if err := o.ironcoreClient.Get(ctx, loadBalancerKey, loadBalancer); err != nil {
		return fmt.Errorf("failed to get LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), err)
	}

	loadBalancerRouting := &networkingv1alpha1.LoadBalancerRouting{}
	loadBalancerRoutingKey := client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}
	if err := o.ironcoreClient.Get(ctx, loadBalancerRoutingKey, loadBalancerRouting); err != nil {
		return fmt.Errorf("failed to get LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), client.ObjectKeyFromObject(loadBalancerRouting), err)
	}

	klog.V(2).InfoS("Updating LoadBalancerRouting destinations for LoadBalancer", "LoadBalancerRouting", client.ObjectKeyFromObject(loadBalancerRouting), "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	loadBalancerDestinations, err := o.getLoadBalancerDestinationsForNodes(ctx, nodes, loadBalancer.Spec.NetworkRef.Name)
	if err != nil {
		return fmt.Errorf("failed to get NetworkInterfaces for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), err)
	}
	loadBalancerRoutingBase := loadBalancerRouting.DeepCopy()
	loadBalancerRouting.Destinations = loadBalancerDestinations

	if err := o.ironcoreClient.Patch(ctx, loadBalancerRouting, client.MergeFrom(loadBalancerRoutingBase)); err != nil {
		return fmt.Errorf("failed to patch LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancerRouting), client.ObjectKeyFromObject(loadBalancer), err)
	}

	klog.V(2).InfoS("Updated LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))
	return nil
}

func (o *ironcoreLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	loadBalancerName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		return fmt.Errorf("failed to get LoadBalancer name for Service %s: %w", client.ObjectKeyFromObject(service), err)
	}

	loadBalancer := &networkingv1alpha1.LoadBalancer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: o.ironcoreNamespace,
			Name:      loadBalancerName,
		},
	}
	klog.V(2).InfoS("Deleting LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	if err := o.ironcoreClient.Delete(ctx, loadBalancer); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("LoadBalancer is already gone", client.ObjectKeyFromObject(loadBalancer))
			return nil
		}
		return fmt.Errorf("failed to delete loadbalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), err)
	}
	if err := waitForDeletingLoadBalancer(ctx, service, o.ironcoreClient, loadBalancer); err != nil {
		return err
	}
	return nil
}

func waitForDeletingLoadBalancer(ctx context.Context, service *v1.Service, ironcoreClient client.Client, loadBalancer *networkingv1alpha1.LoadBalancer) error {
	klog.V(2).InfoS("Waiting for LoadBalancer instance to be deleted", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    waitLoadbalancerActiveSteps,
	}

	if err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		if err := ironcoreClient.Get(ctx, client.ObjectKey{Namespace: loadBalancer.Namespace, Name: loadBalancer.Name}, loadBalancer); !apierrors.IsNotFound(err) {
			return false, err
		}
		return true, nil
	}); wait.Interrupted(err) {
		return fmt.Errorf("timeout waiting for the LoadBalancer %s to be deleted", client.ObjectKeyFromObject(loadBalancer))
	}

	klog.V(2).InfoS("Deleted LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	return nil
}
