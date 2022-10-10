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

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type onmetalClusters struct {
	client.Client
	namespace string
}

func newOnmetalClusters(client client.Client, namespace string) cloudprovider.Clusters {
	return &onmetalClusters{
		Client:    client,
		namespace: namespace,
	}
}

func (o *onmetalClusters) ListClusters(ctx context.Context) ([]string, error) {
	clusterList := &clusterapiv1beta1.ClusterList{}
	if err := o.List(ctx, clusterList); err != nil {
		return nil, errors.Wrap(err, "unable to get cluster list")
	}

	clusterNames := make([]string, len(clusterList.Items))
	for i, clusterItem := range clusterList.Items {
		clusterNames[i] = clusterItem.Name
	}

	return clusterNames, nil
}

func (o *onmetalClusters) Master(ctx context.Context, clusterName string) (string, error) {
	cluster := &clusterapiv1beta1.Cluster{}
	if err := o.Get(ctx, types.NamespacedName{Name: clusterName}, cluster); err != nil {
		return "", errors.Wrap(err, "unable to get cluster")
	}

	return cluster.Spec.ControlPlaneEndpoint.Host, nil
}
