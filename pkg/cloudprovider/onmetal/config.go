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
	"fmt"
	"io"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

type onmetalCloudProviderConfig struct {
	RestConfig  *rest.Config
	Namespace   string
	NetworkName string
}

type CloudConfig struct {
	Namespace   string `json:"namespace"`
	NetworkName string `json:"network-name"`
	Kubeconfig  string `json:"kubeconfig"`
}

func NewConfig(f io.Reader) (*onmetalCloudProviderConfig, error) {
	klog.V(2).Infof("Reading configuration for cloud provider: %s", CloudProviderName)
	configBytes, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "unable to read in config")
	}

	cloudConfig := &CloudConfig{}
	if err := yaml.Unmarshal(configBytes, cloudConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cloud config: %w", err)
	}

	if cloudConfig.Namespace == "" {
		return nil, fmt.Errorf("namespace missing in cloud config")
	}

	if cloudConfig.NetworkName == "" {
		return nil, fmt.Errorf("network-name missing in cloud config")
	}

	if cloudConfig.Kubeconfig == "" {
		return nil, fmt.Errorf("no kubeconfig for the onmetal cluster provided")
	}

	kubeConfig, err := clientcmd.Load([]byte(cloudConfig.Kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("unable to read onmetal cluster kubeconfig: %w", err)
	}
	clientConfig := clientcmd.NewDefaultClientConfig(*kubeConfig, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize onmetal cluster kubeconfig: %w", err)
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to get onmetal cluster rest config: %w", err)
	}

	klog.V(2).Infof("Successfully read configuration for cloud provider: %s", CloudProviderName)

	return &onmetalCloudProviderConfig{
		RestConfig:  restConfig,
		Namespace:   cloudConfig.Namespace,
		NetworkName: cloudConfig.NetworkName,
	}, nil
}
