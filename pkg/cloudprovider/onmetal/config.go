package onmetal

import (
	"io"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type onmetalCloudProviderConfig struct {
	restConfig *rest.Config
	namespace  string
}

func NewConfig(f io.Reader) (*onmetalCloudProviderConfig, error) {
	configBytes, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "unable to read in config")
	}
	kubeConfig, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil, errors.Wrap(err, "unable to read kubeconfig")
	}
	currentContextName := kubeConfig.CurrentContext
	currentContext, ok := kubeConfig.Contexts[currentContextName]
	if !ok {
		return nil, errors.Wrap(err, "default context in kubeconfig is not set, don't know which namespace to use")
	}
	namespace := currentContext.Namespace
	clientConfig := clientcmd.NewDefaultClientConfig(*kubeConfig, nil)
	if err != nil {
		return nil, errors.Wrap(err, "unable to serialize kubeconfig")
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrap(err, "unable to get rest config")
	}
	return &onmetalCloudProviderConfig{
		restConfig: restConfig,
		namespace:  namespace,
	}, nil
}
