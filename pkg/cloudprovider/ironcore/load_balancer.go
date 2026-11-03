// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/api"
	"k8s.io/klog/v2"

	ipamv1alpha1ac "github.com/ironcore-dev/ironcore/client-go/applyconfigurations/ipam/v1alpha1"
	networkingv1alpha1ac "github.com/ironcore-dev/ironcore/client-go/applyconfigurations/networking/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
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
	useNicSelector    bool
}

func newIroncoreLoadBalancer(targetClient client.Client, ironcoreClient client.Client, namespace string, cloudConfig CloudConfig) cloudprovider.LoadBalancer {
	return &ironcoreLoadBalancer{
		targetClient:      targetClient,
		ironcoreClient:    ironcoreClient,
		ironcoreNamespace: namespace,
		cloudConfig:       cloudConfig,
		useNicSelector:    useNicSelector,
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
			return strings.ReplaceAll(string(service.UID), "-", ""), nil
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

func (o *ironcoreLoadBalancer) applyLoadBalancer(
	ctx context.Context,
	clusterName string,
	service *v1.Service,
) (*networkingv1alpha1.LoadBalancer, error) {
	desiredLoadBalancerType := o.getDesiredLoadBalancerType(service)

	loadBalancerName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		return nil, fmt.Errorf("getting load balancer name for service %s: %w",
			client.ObjectKeyFromObject(service), err)
	}

	if err := o.cleanupOutdatedLoadBalancerIfExists(ctx, loadBalancerName, desiredLoadBalancerType); err != nil {
		return nil, fmt.Errorf("cleaning up outdated load balancer: %w", err)
	}

	loadBalancerApplyConfig, err := o.buildLoadBalancerApplyConfig(clusterName, service, loadBalancerName, desiredLoadBalancerType)
	if err != nil {
		return nil, err
	}

	klog.V(2).InfoS("Applying LoadBalancer for Service",
		"loadBalancerName", loadBalancerName,
		"Service", klog.KObj(service),
	)

	if err := o.ironcoreClient.Apply(ctx, loadBalancerApplyConfig, loadBalancerFieldOwner, client.ForceOwnership); err != nil {
		return nil, fmt.Errorf("applying load balancer %s for service %s: %w",
			loadBalancerName, client.ObjectKeyFromObject(service), err)
	}

	klog.V(2).InfoS("Applied LoadBalancer for Service",
		"loadBalancerName", loadBalancerName,
		"Service", klog.KObj(service),
	)

	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	loadBalancerKey := client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}
	if err := o.ironcoreClient.Get(ctx, loadBalancerKey, loadBalancer); err != nil {
		return nil, fmt.Errorf("getting load balancer %s: %w", loadBalancerName, err)
	}
	return loadBalancer, nil
}

func loadBalancerStatusFromLoadBalancer(loadBalancer *networkingv1alpha1.LoadBalancer) *v1.LoadBalancerStatus {
	var lbIngress []v1.LoadBalancerIngress
	for _, ipAddr := range loadBalancer.Status.IPs {
		lbIngress = append(lbIngress, v1.LoadBalancerIngress{IP: ipAddr.String()})
	}
	return &v1.LoadBalancerStatus{
		Ingress: lbIngress,
	}
}

func (o *ironcoreLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	klog.V(2).InfoS("EnsureLoadBalancer for Service", "Cluster", clusterName, "Service", client.ObjectKeyFromObject(service))

	loadBalancer, err := o.applyLoadBalancer(ctx, clusterName, service)
	if err != nil {
		return nil, fmt.Errorf("applying load balancer: %w", err)
	}

	// if useNicSelector is set to false then cloudProvider has to create the loadbalancerRouting
	// if useNicSelector is set to true then Ironcore will take care of creating the loadbalancerRouting
	if !o.useNicSelector {
		if err := o.applyLoadBalancerRoutingForLoadBalancer(ctx, loadBalancer, nodes); err != nil {
			return nil, err
		}

		klog.V(2).InfoS("Applied LoadBalancerRouting for LoadBalancer",
			"loadBalancer", klog.KObj(loadBalancer))
	}

	active, err := o.isLoadBalancerActive(loadBalancer)
	if err != nil {
		return nil, fmt.Errorf("checking if load balancer is active: %w", err)
	}
	if !active {
		klog.V(2).InfoS("Load balancer is not yet active", "loadBalancer", klog.KObj(loadBalancer))
		return nil, api.NewRetryError(fmt.Sprintf("load balancer %s is not yet active", loadBalancer.Name), waitLoadbalancerInitDelay)
	}
	return loadBalancerStatusFromLoadBalancer(loadBalancer), nil
}

func (o *ironcoreLoadBalancer) getDesiredLoadBalancerType(service *v1.Service) networkingv1alpha1.LoadBalancerType {
	if value, ok := service.Annotations[InternalLoadBalancerAnnotation]; ok && value == "true" {
		return networkingv1alpha1.LoadBalancerTypeInternal
	}

	return networkingv1alpha1.LoadBalancerTypePublic
}

func (o *ironcoreLoadBalancer) cleanupOutdatedLoadBalancerIfExists(
	ctx context.Context,
	loadBalancerName string,
	desiredType networkingv1alpha1.LoadBalancerType,
) error {
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	loadBalancerKey := client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}
	if err := o.ironcoreClient.Get(ctx, loadBalancerKey, loadBalancer); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	existingType := loadBalancer.Spec.Type

	if existingType != desiredType {
		if err := o.ensureLoadBalancerDeleted(ctx, loadBalancerName); err != nil {
			return fmt.Errorf("deleting mismatching load balancer %s: %w", loadBalancerKey, err)
		}
	}

	return nil
}

func (o *ironcoreLoadBalancer) buildLoadBalancerApplyConfig(
	clusterName string,
	service *v1.Service,
	loadBalancerName string,
	loadBalancerType networkingv1alpha1.LoadBalancerType,
) (*networkingv1alpha1ac.LoadBalancerApplyConfiguration, error) {
	lbPorts := make([]*networkingv1alpha1ac.LoadBalancerPortApplyConfiguration, 0, len(service.Spec.Ports))

	for _, svcPort := range service.Spec.Ports {
		protocol := svcPort.Protocol

		lbPorts = append(lbPorts, networkingv1alpha1ac.LoadBalancerPort().
			WithProtocol(protocol).
			WithPort(svcPort.Port),
		)
	}

	spec := networkingv1alpha1ac.LoadBalancerSpec().
		WithType(loadBalancerType).
		WithIPFamilies(service.Spec.IPFamilies...).
		WithNetworkRef(v1.LocalObjectReference{Name: o.cloudConfig.NetworkName}).
		WithPorts(lbPorts...)

	if loadBalancerType == networkingv1alpha1.LoadBalancerTypeInternal {
		if o.cloudConfig.PrefixName == "" {
			return nil, fmt.Errorf("prefixName is not defined in config")
		}

		spec.WithIPs(
			networkingv1alpha1ac.IPSource().
				WithEphemeral(
					networkingv1alpha1ac.EphemeralPrefixSource().
						WithPrefixTemplate(
							ipamv1alpha1ac.PrefixTemplateSpec().
								WithSpec(
									ipamv1alpha1ac.PrefixSpec().
										// TODO: for now we only support IPv4 until Gardener has support for IPv6 based Shoots
										WithIPFamily(v1.IPv4Protocol).
										WithParentRef(
											v1.LocalObjectReference{
												Name: o.cloudConfig.PrefixName,
											}),
								),
						),
				),
		)
	}

	if o.useNicSelector {
		spec.WithNetworkInterfaceSelector(
			metav1ac.LabelSelector().
				WithMatchLabels(map[string]string{
					LabelKeyClusterName: clusterName,
				}),
		)
	}

	return networkingv1alpha1ac.LoadBalancer(
		loadBalancerName,
		o.ironcoreNamespace,
	).
		WithAnnotations(map[string]string{
			AnnotationKeyClusterName:      clusterName,
			AnnotationKeyServiceName:      service.Name,
			AnnotationKeyServiceNamespace: service.Namespace,
			AnnotationKeyServiceUID:       string(service.UID),
		}).
		WithSpec(spec), nil
}

func (o *ironcoreLoadBalancer) isLoadBalancerActive(
	loadBalancer *networkingv1alpha1.LoadBalancer,
) (bool, error) {
	if len(loadBalancer.Status.IPs) == 0 {
		return false, nil
	}

	ipRequests := make([]netip.Prefix, 0, len(loadBalancer.Status.IPs))
	ipFamilyToRequest := map[v1.IPFamily]netip.Prefix{
		v1.IPv4Protocol: netip.PrefixFrom(netip.IPv4Unspecified(), 0),
		v1.IPv6Protocol: netip.PrefixFrom(netip.IPv6Unspecified(), 0),
	}

	switch loadBalancer.Spec.Type {
	case networkingv1alpha1.LoadBalancerTypePublic:
		for _, ipFamily := range loadBalancer.Spec.IPFamilies {
			ipRequests = append(ipRequests, ipFamilyToRequest[ipFamily])
		}
	case networkingv1alpha1.LoadBalancerTypeInternal:
		for _, ipSrc := range loadBalancer.Spec.IPs {
			switch {
			case ipSrc.Value != nil:
				ip := ipSrc.Value
				ipRequests = append(ipRequests, netip.PrefixFrom(ip.Addr, ip.BitLen()))
			case ipSrc.Ephemeral != nil:
				prefixSpec := ipSrc.Ephemeral.PrefixTemplate.Spec
				ipRequests = append(ipRequests, ipFamilyToRequest[prefixSpec.IPFamily])
			}
		}
	}

	// Sort descending, higher specificity over lower
	slices.SortFunc(ipRequests, func(a, b netip.Prefix) int { return -a.Compare(b) })

	unmatched := make(map[netip.Addr]struct{}, len(loadBalancer.Status.IPs))
	for _, ip := range loadBalancer.Status.IPs {
		unmatched[ip.Addr] = struct{}{}
	}

Loop:
	for _, request := range ipRequests {
		for ip := range unmatched {
			if request.Contains(ip) {
				delete(unmatched, ip)
				continue Loop
			}
		}
		return false, nil
	}
	return true, nil
}

func (o *ironcoreLoadBalancer) applyLoadBalancerRoutingForLoadBalancer(ctx context.Context, loadBalancer *networkingv1alpha1.LoadBalancer, nodes []*v1.Node) error {
	loadBalancerDestinations, err := o.getLoadBalancerDestinationsForNodes(ctx, nodes, loadBalancer.Spec.NetworkRef.Name)
	if err != nil {
		return fmt.Errorf("failed to get NetworkInterfaces for Nodes: %w", err)
	}

	network := &networkingv1alpha1.Network{}
	networkKey := client.ObjectKey{
		Namespace: o.ironcoreNamespace,
		Name:      loadBalancer.Spec.NetworkRef.Name,
	}

	if err := o.ironcoreClient.Get(ctx, networkKey, network); err != nil {
		return fmt.Errorf("failed to get Network %s: %w", o.cloudConfig.NetworkName, err)
	}

	loadBalancerRoutingApplyConfig := o.buildLoadBalancerRoutingApplyConfig(loadBalancer, network, loadBalancerDestinations)

	if err := o.ironcoreClient.Apply(ctx, loadBalancerRoutingApplyConfig, client.FieldOwner(loadBalancerFieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancer.Name}, client.ObjectKeyFromObject(loadBalancer), err)
	}

	return nil
}

func (o *ironcoreLoadBalancer) buildLoadBalancerRoutingApplyConfig(
	loadBalancer *networkingv1alpha1.LoadBalancer,
	network *networkingv1alpha1.Network,
	destinations []networkingv1alpha1.LoadBalancerDestination,
) *networkingv1alpha1ac.LoadBalancerRoutingApplyConfiguration {
	destinationApplyConfigs := make([]*networkingv1alpha1ac.LoadBalancerDestinationApplyConfiguration, 0, len(destinations))

	for _, destination := range destinations {
		targetRef := networkingv1alpha1ac.LoadBalancerTargetRef().
			WithName(destination.TargetRef.Name).
			WithUID(destination.TargetRef.UID)
		if destination.TargetRef.ProviderID != "" {
			targetRef = targetRef.WithProviderID(destination.TargetRef.ProviderID)
		}

		destinationApplyConfigs = append(
			destinationApplyConfigs,
			networkingv1alpha1ac.LoadBalancerDestination().
				WithIP(destination.IP).
				WithTargetRef(targetRef),
		)
	}

	return networkingv1alpha1ac.LoadBalancerRouting(
		loadBalancer.Name,
		o.ironcoreNamespace,
	).
		WithOwnerReferences(
			metav1ac.OwnerReference().
				WithAPIVersion(networkingv1alpha1.SchemeGroupVersion.String()).
				WithKind("LoadBalancer").
				WithName(loadBalancer.Name).
				WithUID(loadBalancer.UID).
				WithController(true).
				WithBlockOwnerDeletion(true),
		).
		WithNetworkRef(
			v1alpha1.LocalUIDReference{
				Name: network.Name,
				UID:  network.UID,
			}).
		WithDestinations(destinationApplyConfigs...)
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
	if !o.useNicSelector {
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
	}

	klog.V(2).InfoS("Updated LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))
	return nil
}

func (o *ironcoreLoadBalancer) ensureLoadBalancerDeleted(ctx context.Context, loadBalancerName string) error {
	loadBalancer := &networkingv1alpha1.LoadBalancer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: o.ironcoreNamespace,
			Name:      loadBalancerName,
		},
	}
	klog.V(2).InfoS("Deleting LoadBalancer", "loadBalancer", loadBalancerName)
	if err := o.ironcoreClient.Delete(ctx, loadBalancer); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("LoadBalancer is already gone", "loadBalancerName", loadBalancerName)
			return nil
		}
		return fmt.Errorf("deleting loadbalancer %s: %w", loadBalancerName, err)
	}
	return o.waitForLoadBalancerToBeDeleted(ctx, loadBalancerName)
}

func (o *ironcoreLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	loadBalancerName, err := o.getLoadBalancerName(ctx, clusterName, service)
	if err != nil {
		return fmt.Errorf("failed to get LoadBalancer name for Service %s: %w", client.ObjectKeyFromObject(service), err)
	}

	return o.ensureLoadBalancerDeleted(ctx, loadBalancerName)
}

func (o *ironcoreLoadBalancer) waitForLoadBalancerToBeDeleted(
	ctx context.Context,
	loadBalancerName string,
) error {
	klog.V(2).InfoS("Waiting for LoadBalancer instance to be deleted", "loadBalancerName", loadBalancerName)
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    waitLoadbalancerActiveSteps,
	}

	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	loadBalancerKey := client.ObjectKey{Namespace: o.ironcoreNamespace, Name: loadBalancerName}
	if err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		if err := o.ironcoreClient.Get(ctx, loadBalancerKey, loadBalancer); err != nil {
			if !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("getting load balancer %s: %w", loadBalancerName, err)
			}
			return true, nil
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("waiting for the load balancer %s to be deleted: %w", loadBalancerName, err)
	}

	klog.V(2).InfoS("Deleted LoadBalancer", "loadBalancer", loadBalancerName)
	return nil
}
