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

	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalRoutes struct {
	client.Client
}

func newOnmetalRoutes(client client.Client) cloudprovider.Routes {
	return &onmetalRoutes{
		Client: client,
	}
}

func (o *onmetalRoutes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	return nil, cloudprovider.NotImplemented
}

func (o *onmetalRoutes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	return cloudprovider.NotImplemented
}

func (o *onmetalRoutes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	return cloudprovider.NotImplemented
}
