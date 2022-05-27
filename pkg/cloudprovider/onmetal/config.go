package onmetal

import (
	"io"
	"io/ioutil"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type onmetalCloudProviderConfig struct {
	restConfig *rest.Config
}

func NewConfig(f io.Reader) (*onmetalCloudProviderConfig, error) {
	configBytes, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "unable to read in config")
	}
	kubeConfig, err := clientcmd.Load(configBytes)
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
	}, nil
}
