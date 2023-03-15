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
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/onmetal/onmetal-api/api/common/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
)

const (
	waitLoadbalancerInitDelay   = 1 * time.Second
	waitLoadbalancerFactor      = 1.2
	waitLoadbalancerActiveSteps = 19
)

var (
	loadBalancerFieldOwner = client.FieldOwner("cloud-provider.onmetal.de/loadbalancer")
)

type onmetalLoadBalancer struct {
	targetClient     client.Client
	onmetalClient    client.Client
	onmetalNamespace string
	networkName      string
}

func newOnmetalLoadBalancer(targetClient client.Client, onmetalClient client.Client, namespace string, networkName string) cloudprovider.LoadBalancer {
	return &onmetalLoadBalancer{
		targetClient:     targetClient,
		onmetalClient:    onmetalClient,
		onmetalNamespace: namespace,
		networkName:      networkName,
	}
}

func (o *onmetalLoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	loadBalancerName := o.GetLoadBalancerName(ctx, clusterName, service)
	klog.V(2).InfoS("Getting LoadBalancer %s", loadBalancerName)

	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	loadBalancerKey := client.ObjectKey{Namespace: o.onmetalNamespace, Name: loadBalancerName}
	if err = o.onmetalClient.Get(ctx, loadBalancerKey, loadBalancer); err != nil {
		return nil, false, fmt.Errorf("failed to get LoadBalancer %s for Service %s: %w", loadBalancerName, client.ObjectKeyFromObject(service), err)
	}

	lbAllocatedIps := loadBalancer.Status.IPs
	status = &v1.LoadBalancerStatus{}
	for _, ip := range lbAllocatedIps {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ip.String()})
	}
	return status, true, nil
}

func (o *onmetalLoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	return getLoadBalancerName(clusterName, service)
}

func (o *onmetalLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	klog.V(2).InfoS("EnsureLoadBalancer for Service", "Cluster", clusterName, "Service", client.ObjectKeyFromObject(service))

	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are no available nodes for LoadBalancer service %s", client.ObjectKeyFromObject(service))
	}

	loadBalancerName := getLoadBalancerName(clusterName, service)
	klog.V(2).InfoS("Getting LoadBalancer ports from Service", "Service", client.ObjectKeyFromObject(service))
	lbPorts := []networkingv1alpha1.LoadBalancerPort{}
	for _, svcPort := range service.Spec.Ports {
		lbPorts = append(lbPorts, networkingv1alpha1.LoadBalancerPort{
			Protocol: &svcPort.Protocol,
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
			Namespace: o.onmetalNamespace,
			Annotations: map[string]string{
				AnnotationKeyClusterName:      clusterName,
				AnnotationKeyServiceName:      service.Name,
				AnnotationKeyServiceNamespace: service.Namespace,
				AnnotationKeyServiceUID:       string(service.UID),
			},
		},
		Spec: networkingv1alpha1.LoadBalancerSpec{
			Type:       networkingv1alpha1.LoadBalancerTypePublic,
			IPFamilies: service.Spec.IPFamilies,
			NetworkRef: v1.LocalObjectReference{
				Name: o.networkName,
			},
			Ports: lbPorts,
		},
	}

	klog.V(2).InfoS("Applying LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))
	if err := o.onmetalClient.Patch(ctx, loadBalancer, client.Apply, loadBalancerFieldOwner, client.ForceOwnership); err != nil {
		return nil, fmt.Errorf("failed to apply LoadBalancer %s for Service %s: %w", client.ObjectKeyFromObject(loadBalancer), client.ObjectKeyFromObject(service), err)
	}
	klog.V(2).InfoS("Applied LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))

	klog.V(2).InfoS("Applying LoadBalancerRouting for LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	if err := o.applyLoadBalancerRoutingForLoadBalancer(ctx, loadBalancer, nodes); err != nil {
		return nil, err
	}
	klog.V(2).InfoS("Applied LoadBalancerRouting for LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))

	lbIPs := loadBalancer.Status.IPs
	if len(lbIPs) == 0 {
		if err := waitLoadBalancerActive(ctx, clusterName, service, o.onmetalClient, loadBalancer); err != nil {
			return nil, err
		}
	}

	lbIngress := []v1.LoadBalancerIngress{}
	for _, ipAddr := range loadBalancer.Status.IPs {
		lbIngress = append(lbIngress, v1.LoadBalancerIngress{IP: ipAddr.String()})
	}

	return &v1.LoadBalancerStatus{Ingress: lbIngress}, nil
}

func getLoadBalancerName(clusterName string, service *v1.Service) string {
	nameSuffix := strings.Split(string(service.UID), "-")[0]
	return fmt.Sprintf("%s-%s-%s", clusterName, service.Name, nameSuffix)
}

func waitLoadBalancerActive(ctx context.Context, clusterName string, service *v1.Service, onmetalClient client.Client, loadBalancer *networkingv1alpha1.LoadBalancer) error {
	klog.V(2).InfoS("Waiting for onmetal LoadBalancer to become ready", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    waitLoadbalancerActiveSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		err := onmetalClient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: string(loadBalancer.Name)}, loadBalancer)
		if err == nil {
			if len(loadBalancer.Status.IPs) > 0 {
				return true, nil
			}
		}
		return false, err
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("timeout waiting for the onmetal LoadBalancer %s to become ready", client.ObjectKeyFromObject(loadBalancer))
	}

	klog.V(2).InfoS("onmetal LoadBalancer became ready", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	return err
}

func (o *onmetalLoadBalancer) applyLoadBalancerRoutingForLoadBalancer(ctx context.Context, loadBalancer *networkingv1alpha1.LoadBalancer, nodes []*v1.Node) error {
	networkInterfaces, err := o.getNetworkInterfacesForNode(ctx, nodes, loadBalancer.Spec.NetworkRef.Name)
	if err != nil {
		return fmt.Errorf("failed to get NetworkInterfaces for Nodes: %w", err)
	}

	network := &networkingv1alpha1.Network{}
	networkKey := client.ObjectKey{Namespace: o.onmetalNamespace, Name: loadBalancer.Spec.NetworkRef.Name}
	if err := o.onmetalClient.Get(ctx, networkKey, network); err != nil {
		return fmt.Errorf("failed to get Network %s: %w", o.networkName, err)
	}

	loadBalancerRouting := &networkingv1alpha1.LoadBalancerRouting{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LoadBalancerRouting",
			APIVersion: networkingv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancer.Name,
			Namespace: o.onmetalNamespace,
		},
		NetworkRef: commonv1alpha1.LocalUIDReference{
			Name: network.Name,
			UID:  network.UID,
		},
		Destinations: networkInterfaces,
	}

	if err := o.onmetalClient.Patch(ctx, loadBalancerRouting, client.Apply, loadBalancerFieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancerRouting), client.ObjectKeyFromObject(loadBalancer), err)
	}
	return nil
}

func (o *onmetalLoadBalancer) getNetworkInterfacesForNode(ctx context.Context, nodes []*v1.Node, networkName string) ([]commonv1alpha1.LocalUIDReference, error) {
	var networkInterfaces = []commonv1alpha1.LocalUIDReference{}
	for _, node := range nodes {
		machine, err := GetMachineForNode(ctx, o.onmetalClient, o.onmetalNamespace, node)
		if err != nil {
			return networkInterfaces, err
		}

		for _, machineNIC := range machine.Spec.NetworkInterfaces {
			networkInterface := &networkingv1alpha1.NetworkInterface{}
			networkInterfaceName := fmt.Sprintf("%s-%s", machine.Name, machineNIC.Name)
			if err := o.onmetalClient.Get(ctx, client.ObjectKey{Namespace: o.onmetalNamespace, Name: networkInterfaceName}, networkInterface); err != nil {
				return nil, fmt.Errorf("failed to get network interface %s for machine %s: %w", client.ObjectKeyFromObject(networkInterface), client.ObjectKeyFromObject(machine), err)
			}
			if networkInterface.Spec.NetworkRef.Name == networkName {
				networkInterfaces = append(networkInterfaces, commonv1alpha1.LocalUIDReference{
					Name: networkInterface.Name,
					UID:  networkInterface.UID,
				})
			}
		}
	}
	return networkInterfaces, nil
}

func (o *onmetalLoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	klog.V(2).InfoS("Updating LoadBalancer for Service", "Service", client.ObjectKeyFromObject(service))
	if len(nodes) == 0 {
		return fmt.Errorf("no Nodes available for LoadBalancer Service %s", client.ObjectKeyFromObject(service))
	}

	loadBalancerName := o.GetLoadBalancerName(ctx, clusterName, service)
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	loadBalancerKey := client.ObjectKey{Namespace: o.onmetalNamespace, Name: loadBalancerName}
	if err := o.onmetalClient.Get(ctx, loadBalancerKey, loadBalancer); err != nil {
		return fmt.Errorf("failed to get LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), err)
	}

	loadBalancerRouting := &networkingv1alpha1.LoadBalancerRouting{}
	loadBalancerRoutingKey := client.ObjectKey{Namespace: o.onmetalNamespace, Name: loadBalancerName}
	if err := o.onmetalClient.Get(ctx, loadBalancerRoutingKey, loadBalancerRouting); err != nil {
		return fmt.Errorf("failed to get LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), client.ObjectKeyFromObject(loadBalancerRouting), err)
	}

	klog.V(2).InfoS("Updating LoadBalancerRouting destinations for LoadBalancer", "LoadBalancerRouting", client.ObjectKeyFromObject(loadBalancerRouting), "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	networkInterfaces, err := o.getNetworkInterfacesForNode(ctx, nodes, loadBalancer.Spec.NetworkRef.Name)
	if err != nil {
		return fmt.Errorf("failed to get NetworkInterfaces for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), err)
	}
	loadBalancerRoutingBase := loadBalancerRouting.DeepCopy()
	loadBalancerRouting.Destinations = networkInterfaces

	if err := o.onmetalClient.Patch(ctx, loadBalancerRouting, client.MergeFrom(loadBalancerRoutingBase)); err != nil {
		return fmt.Errorf("failed to patch LoadBalancerRouting %s for LoadBalancer %s: %w", client.ObjectKeyFromObject(loadBalancerRouting), client.ObjectKeyFromObject(loadBalancer), err)
	}

	klog.V(2).InfoS("Updated LoadBalancer for Service", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer), "Service", client.ObjectKeyFromObject(service))
	return nil
}

func (o *onmetalLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	loadBalancerName := o.GetLoadBalancerName(ctx, clusterName, service)
	loadBalancer := &networkingv1alpha1.LoadBalancer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: o.onmetalNamespace,
			Name:      loadBalancerName,
		},
	}
	klog.V(2).InfoS("Deleting LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	if err := o.onmetalClient.Delete(ctx, loadBalancer); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete loadbalancer %s: %w", client.ObjectKeyFromObject(loadBalancer), err)
	}
	klog.V(2).InfoS("Deleted LoadBalancer", "LoadBalancer", client.ObjectKeyFromObject(loadBalancer))
	return nil
}
