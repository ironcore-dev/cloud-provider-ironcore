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

	"github.com/onmetal/onmetal-api/api/common/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalLoadBalancer struct {
	targetClient     client.Client
	onmetalClient    client.Client
	onmetalNamespace string
	networkName      string
}

const (
	waitLoadbalancerInitDelay   = 1 * time.Second
	waitLoadbalancerFactor      = 1.2
	waitLoadbalancerActiveSteps = 19
)

var (
	loadbalancerFieldOwner = client.FieldOwner("cloud-provider.onmetal.de/loadbalancer")
)

func newOnmetalLoadBalancer(targetClient client.Client, onmetalClient client.Client, namespace string, networkName string) cloudprovider.LoadBalancer {
	return &onmetalLoadBalancer{
		targetClient:     targetClient,
		onmetalClient:    onmetalClient,
		onmetalNamespace: namespace,
		networkName:      networkName,
	}
}

func (o *onmetalLoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	lbName := o.GetLoadBalancerName(ctx, clusterName, service)
	klog.V(4).Infof("Getting loadbalancer %s", lbName)
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	err = o.onmetalClient.Get(ctx, types.NamespacedName{Namespace: o.onmetalNamespace, Name: lbName}, loadBalancer)

	if apierrors.IsNotFound(err) {
		return nil, false, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to get loadbalancer %s for service %s: %w", lbName, service.Name, err)
	}

	lbAllocatedIps := loadBalancer.Status.IPs
	status = &v1.LoadBalancerStatus{}
	for _, ip := range lbAllocatedIps {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ip.GomegaString()})
	}
	return status, true, nil
}

func (o *onmetalLoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	return getLoadBalancerName(clusterName, service)
}

func (o *onmetalLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	klog.V(2).InfoS("EnsureLoadBalancer", "cluster", clusterName, "service", klog.KObj(service))

	klog.V(4).Info("EnsureLoadBalancer - check if nodes are present")
	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are no available nodes for LoadBalancer service %s", service.Name)
	}

	klog.V(4).Info("EnsureLoadBalancer - get loadbalancer name")
	lbName := getLoadBalancerName(clusterName, service)

	klog.V(4).Info("EnsureLoadBalancer - get service ports and protocols")
	lbPorts := []networkingv1alpha1.LoadBalancerPort{}
	for _, svcPort := range service.Spec.Ports {
		lbPorts = append(lbPorts, networkingv1alpha1.LoadBalancerPort{
			Protocol: &svcPort.Protocol,
			Port:     svcPort.Port,
		})
	}

	klog.V(4).Info("EnsureLoadBalancer - Create loadBalancer for service")
	loadBalancer := &networkingv1alpha1.LoadBalancer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LoadBalancer",
			APIVersion: networkingv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbName,
			Namespace: o.onmetalNamespace,
			Annotations: map[string]string{
				"cluster-name":      clusterName,
				"service-name":      service.Name,
				"service-namespace": service.Namespace,
				"service-uid":       string(service.UID),
			},
		},
		Spec: networkingv1alpha1.LoadBalancerSpec{
			Type:       networkingv1alpha1.LoadBalancerTypePublic,
			IPFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			NetworkRef: v1.LocalObjectReference{
				Name: o.networkName,
			},
			Ports: lbPorts,
		},
	}

	if err := o.onmetalClient.Patch(ctx, loadBalancer, client.Apply, client.FieldOwner(loadbalancerFieldOwner), client.ForceOwnership); err != nil {
		return nil, err
	}
	klog.V(4).Infof("EnsureLoadBalancer - ensured loadbalancer %s successfully", loadBalancer.Name)

	if err := o.ensureLoadBalancerRouting(ctx, loadBalancer, nodes); err != nil {
		return nil, err
	}
	klog.V(4).Infof("EnsureLoadBalancer - ensured LoadBalancerRouting %s successfully", loadBalancer.Name)

	lbIPs := loadBalancer.Status.IPs
	if len(lbIPs) == 0 {
		if err := WaitLoadbalancerActive(ctx, clusterName, service, o.onmetalClient, loadBalancer); err != nil {
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

func WaitLoadbalancerActive(ctx context.Context, clusterName string, service *v1.Service, onmetalClient client.Client, loadBalancer *networkingv1alpha1.LoadBalancer) error {
	klog.V(4).InfoS("Waiting for onmetal load balancer is ready", "lbName", loadBalancer.Name)
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
		err = fmt.Errorf("timeout waiting for the onmetal loadbalancer %s", loadBalancer.Name)
	}

	return err
}

func (o *onmetalLoadBalancer) ensureLoadBalancerRouting(ctx context.Context, loadBalancer *networkingv1alpha1.LoadBalancer, nodes []*v1.Node) error {
	klog.V(2).Info("ensureLoadBalancerRouting - create LoadBalancerRouting for ", loadBalancer.Name)

	var lbrNwInterfaces = []v1alpha1.LocalUIDReference{}
	for _, node := range nodes {
		machine, err := GetMachineForNode(ctx, o.onmetalClient, o.onmetalNamespace, node)
		if err != nil {
			return err
		}

		for _, nwiface := range machine.Spec.NetworkInterfaces {
			nwiTemplate := nwiface.Ephemeral.NetworkInterfaceTemplate
			nwiNwName := nwiTemplate.Spec.NetworkRef.Name
			if o.networkName == nwiNwName {
				lbrNwInterfaces = append(lbrNwInterfaces, v1alpha1.LocalUIDReference{
					Name: nwiface.Name,
					UID:  nwiTemplate.UID,
				})
			}
		}
	}

	network := &networkingv1alpha1.Network{}
	err := o.onmetalClient.Get(ctx, types.NamespacedName{Namespace: o.onmetalNamespace, Name: string(o.networkName)}, network)
	if err != nil {
		return fmt.Errorf("failed to get network %s: %w", o.networkName, err)
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
		NetworkRef: v1alpha1.LocalUIDReference{
			Name: o.networkName,
			UID:  network.UID,
		},
		Destinations: lbrNwInterfaces,
	}

	if err := ctrl.SetControllerReference(loadBalancer, loadBalancerRouting, o.onmetalClient.Scheme()); err != nil {
		return fmt.Errorf("failed to set ownerreference for LoadBalancerRouting %s: %w", client.ObjectKeyFromObject(loadBalancerRouting), err)
	}

	return o.onmetalClient.Patch(ctx, loadBalancerRouting, client.Apply, client.FieldOwner(loadbalancerFieldOwner), client.ForceOwnership)
}

func (o *onmetalLoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	lbName := o.GetLoadBalancerName(ctx, clusterName, service)
	klog.V(4).Infof("Deleting loadbalancer instance with name: %s", lbName)
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	err := o.onmetalClient.Get(ctx, types.NamespacedName{Namespace: o.onmetalNamespace, Name: lbName}, loadBalancer)

	if apierrors.IsNotFound(err) {
		return cloudprovider.InstanceNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get loadbalancer %s: %w", lbName, err)
	}

	if err = o.onmetalClient.Delete(ctx, loadBalancer); err != nil {
		return fmt.Errorf("failed to delete loadbalancer %s: %w", lbName, err)
	}

	return nil
}
