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

	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalLoadBalancer struct {
	client.Client
}

func newOnmetalLoadBalancer(client client.Client) cloudprovider.LoadBalancer {
	return &onmetalLoadBalancer{
		Client: client,
	}
}

func (o *onmetalLoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	return nil, false, cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	return ""
}

func (o *onmetalLoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalLoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	return cloudprovider.NotImplemented
}
