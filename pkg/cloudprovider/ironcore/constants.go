// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

const (
	// InternalLoadBalancerAnnotation is internal load balancer annotation of service
	InternalLoadBalancerAnnotation = "service.beta.kubernetes.io/ironcore-load-balancer-internal"
	// AnnotationKeyClusterName is the cluster name annotation key name
	AnnotationKeyClusterName = "cluster-name"
	// AnnotationKeyServiceName is the service name annotation key name
	AnnotationKeyServiceName = "service-name"
	// AnnotationKeyServiceNamespace is the service namespace annotation key name
	AnnotationKeyServiceNamespace = "service-namespace"
	// AnnotationKeyServiceUID is the service UID annotation key name
	AnnotationKeyServiceUID = "service-uid"
	// LabelKeyClusterName is the label key name used to identify the cluster name in Kubernetes labels
	LabelKeyClusterName = "kubernetes.io/cluster"
)
