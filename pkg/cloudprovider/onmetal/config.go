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
	"sigs.k8s.io/yaml"
)

type onmetalCloudProviderConfig struct {
	onmetalRestConfig *rest.Config
	targetRestConfig  *rest.Config
	namespace         string
}

type CloudConfig struct {
	OnmetalClusterKubeconfig string `json:"onmetalClusterKubeconfig"`
	TargetClusterKubeconfig  string `json:"targetClusterKubeconfig"`
}

func NewConfig(f io.Reader) (*onmetalCloudProviderConfig, error) {
	configBytes, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "unable to read in config")
	}

	cloudConfig := &CloudConfig{}
	if err := yaml.Unmarshal(configBytes, cloudConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cloud config: %w", err)
	}

	var onmetalClusterRestConfig *rest.Config
	var targetClusterRestConfig *rest.Config

	if len(cloudConfig.OnmetalClusterKubeconfig) == 0 {
		return nil, fmt.Errorf("no kubeconfig for the onmetal cluster provided")
	} else {
		//kubeConfigData, err := base64.StdEncoding.DecodeString(cloudConfig.OnmetalClusterKubeconfig)
		//if err != nil {
		//	return nil, fmt.Errorf("unable to base64 decode onmetal cluster kubeconfig: %w", err)
		//}
		kubeConfig, err := clientcmd.Load([]byte(cloudConfig.OnmetalClusterKubeconfig))
		if err != nil {
			return nil, fmt.Errorf("unable to read onmetal cluster kubeconfig: %w", err)
		}
		clientConfig := clientcmd.NewDefaultClientConfig(*kubeConfig, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize onmetal cluster kubeconfig: %w", err)
		}
		onmetalClusterRestConfig, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("unable to get onmetal cluster rest config: %w", err)
		}
	}

	// In case the target cluster is equal to the onmetal cluster, we will just reuse the same kubeconfig.
	if len(cloudConfig.TargetClusterKubeconfig) == 0 {
		targetClusterRestConfig = onmetalClusterRestConfig
	} else {
		//kubeConfigData, err := base64.StdEncoding.DecodeString(cloudConfig.TargetClusterKubeconfig)
		//if err != nil {
		//	return nil, fmt.Errorf("unable to base64 decode target cluster kubeconfig: %w", err)
		//}
		kubeConfig, err := clientcmd.Load([]byte(cloudConfig.TargetClusterKubeconfig))
		if err != nil {
			return nil, fmt.Errorf("unable to read target cluster kubeconfig: %w", err)
		}
		clientConfig := clientcmd.NewDefaultClientConfig(*kubeConfig, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize target cluster kubeconfig: %w", err)
		}
		targetClusterRestConfig, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("unable to get target cluster rest config: %w", err)
		}
	}

	return &onmetalCloudProviderConfig{
		onmetalRestConfig: onmetalClusterRestConfig,
		targetRestConfig:  targetClusterRestConfig,
	}, nil
}
