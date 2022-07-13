package onmetal

import (
	"context"
	"fmt"

	onmetalapi "github.com/onmetal/onmetal-api/generated/clientset/versioned"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	cloudprovider "k8s.io/cloud-provider"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	CClusterCRDName = "clusters"
)

type onmetalClusters struct {
	clientSet *onmetalapi.Clientset
	namespace string
}

func newOnmetalClusters(clientSet *onmetalapi.Clientset, namespace string) cloudprovider.Clusters {
	return &onmetalClusters{
		clientSet: clientSet,
		namespace: namespace,
	}
}

func (o *onmetalClusters) ListClusters(ctx context.Context) ([]string, error) {
	clusterList := &clusterapiv1beta1.ClusterList{}
	err := o.clientSet.RESTClient().
		Get().
		Namespace(o.namespace).
		Resource(fmt.Sprintf("%s.%s", CClusterCRDName, clusterapiv1beta1.GroupVersion.Group)).
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(clusterList)

	if err != nil {
		return nil, errors.Wrap(err, "unable to get cluster list")
	}

	clusterNames := make([]string, len(clusterList.Items))
	for i, clusterItem := range clusterList.Items {
		clusterNames[i] = clusterItem.Name
	}

	return clusterNames, nil
}

func (o *onmetalClusters) Master(ctx context.Context, clusterName string) (string, error) {
	clusterItem := &clusterapiv1beta1.Cluster{}
	err := o.clientSet.RESTClient().
		Get().
		Namespace(o.namespace).
		Resource(fmt.Sprintf("%s.%s", CClusterCRDName, clusterapiv1beta1.GroupVersion.Group)).
		Name(clusterName).
		VersionedParams(&metav1.GetOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(clusterItem)

	if err != nil {
		return "", errors.Wrap(err, "unable to get cluster")
	}

	return clusterItem.Spec.ControlPlaneEndpoint.Host, nil
}
