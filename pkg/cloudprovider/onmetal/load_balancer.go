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
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalLoadBalancer struct {
	Client           client.Client
	onmetalNamespace string
}

func newOnmetalLoadBalancer(client client.Client, namespace string) cloudprovider.LoadBalancer {
	return &onmetalLoadBalancer{
		Client:           client,
		onmetalNamespace: namespace,
	}
}

func (o *onmetalLoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	name := o.GetLoadBalancerName(ctx, clusterName, service)
	klog.V(4).Infof("Getting loadbalancer status with name: ", name)
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	err = o.Client.Get(ctx, types.NamespacedName{Namespace: o.onmetalNamespace, Name: string(name)}, loadBalancer)

	if apierrors.IsNotFound(err) {
		return nil, false, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, false, err
	}

	lbAllocatedIps := loadBalancer.Status.IPs
	status = &v1.LoadBalancerStatus{}
	for _, ip := range lbAllocatedIps {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ip.GomegaString()})
	}
	return status, true, nil
}

func (o *onmetalLoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	klog.V(4).Infof("Getting loadbalancer name with cluster name: %s, service name: %s serviceUID: %s\n", clusterName, service.Name, service.UID)
	stringval := getHashValueForUID([]byte(service.UID))
	name := fmt.Sprintf("%s-%s-%s", clusterName, service.Name, stringval)
	return trimLoadBalancerName(name)
}

func (o *onmetalLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	lbName := o.GetLoadBalancerName(ctx, clusterName, service)
	klog.V(4).Infof("Deleting loadbalancer instance with name: ", lbName)
	loadBalancer := &networkingv1alpha1.LoadBalancer{}
	err := o.Client.Get(ctx, types.NamespacedName{Namespace: o.onmetalNamespace, Name: string(lbName)}, loadBalancer)

	if apierrors.IsNotFound(err) {
		return cloudprovider.InstanceNotFound
	}
	if err != nil {
		return err
	}

	err = o.Client.Delete(ctx, loadBalancer)
	if err != nil {
		return err
	}

	return nil
}

// getHashValueForUID returns first 7 chars of after calculating hash of service UID
func getHashValueForUID(uid []byte) string {
	uidHash := sha256.Sum256(uid)
	stringval := hex.EncodeToString(uidHash[:])
	return stringval[0:6]
}

// trimLoadBalancerName makes sure the string length doesn't exceed 63, which is usually the maximum length for LoadBalancer name.
func trimLoadBalancerName(original string) string {
	ret := original
	if len(original) > 62 {
		ret = original[:62]
	}
	return ret
}
