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

package ironcore

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Config", func() {
	It("should load a correct provider config", func() {
		sampleConfig := map[string]string{"networkName": "my-network", "prefixName": "my-prefix", "clusterName": "my-cluster"}
		sampleConfigData, err := yaml.Marshal(sampleConfig)
		Expect(err).NotTo(HaveOccurred())

		kubeconfig := api.Config{
			Kind:       "Config",
			APIVersion: "v1",
			Clusters: map[string]*api.Cluster{"foo": {
				Server:                   "https://server",
				CertificateAuthorityData: []byte("12345"),
			}},
			AuthInfos: map[string]*api.AuthInfo{
				"foo": {
					ClientCertificateData: []byte("12345"),
					ClientKeyData:         []byte("12345"),
					Username:              "user",
				},
			},
			Contexts: map[string]*api.Context{
				"foo": {
					Cluster:   "foo",
					AuthInfo:  "user",
					Namespace: "test",
				},
			},
			CurrentContext: "foo",
		}
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		Expect(err).NotTo(HaveOccurred())

		configReader := strings.NewReader(string(sampleConfigData))
		kubeconfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "kubeconfig")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = kubeconfigFile.Close()
		}()
		Expect(os.WriteFile(kubeconfigFile.Name(), kubeconfigData, 0666)).To(Succeed())
		IroncoreKubeconfigPath = kubeconfigFile.Name()
		config, err := LoadCloudProviderConfig(configReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(config.RestConfig).NotTo(BeNil())
		Expect(config.Namespace).To(Equal("test"))
		Expect(config.cloudConfig.NetworkName).To(Equal("my-network"))
		Expect(config.cloudConfig.PrefixName).To(Equal("my-prefix"))
		Expect(config.cloudConfig.ClusterName).To(Equal("my-cluster"))
	})

	It("should get the default namespace if no namespace was defined for an auth context", func() {
		sampleConfig := map[string]string{"networkName": "my-network", "prefixName": "my-prefix", "clusterName": "my-cluster"}
		sampleConfigData, err := yaml.Marshal(sampleConfig)
		Expect(err).NotTo(HaveOccurred())

		kubeconfig := api.Config{
			Kind:       "Config",
			APIVersion: "v1",
			Clusters: map[string]*api.Cluster{"foo": {
				Server:                   "https://server",
				CertificateAuthorityData: []byte("12345"),
			}},
			AuthInfos: map[string]*api.AuthInfo{
				"foo": {
					ClientCertificateData: []byte("12345"),
					ClientKeyData:         []byte("12345"),
					Username:              "user",
				},
			},
			Contexts: map[string]*api.Context{
				"foo": {
					Cluster:   "foo",
					AuthInfo:  "user",
					Namespace: "",
				},
			},
			CurrentContext: "foo",
		}
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		Expect(err).NotTo(HaveOccurred())

		configReader := strings.NewReader(string(sampleConfigData))
		kubeconfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "kubeconfig")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = kubeconfigFile.Close()
		}()
		Expect(os.WriteFile(kubeconfigFile.Name(), kubeconfigData, 0666)).To(Succeed())
		IroncoreKubeconfigPath = kubeconfigFile.Name()
		config, err := LoadCloudProviderConfig(configReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(config.RestConfig).NotTo(BeNil())
		// TODO: empty or unset namespace will be defaulted to the 'default' namespace. We might want to handle this
		// as an error.
		Expect(config.Namespace).To(Equal("default"))
		Expect(config.cloudConfig.NetworkName).To(Equal("my-network"))
		Expect(config.cloudConfig.PrefixName).To(Equal("my-prefix"))
		Expect(config.cloudConfig.ClusterName).To(Equal("my-cluster"))
	})

	It("should fail on empty networkName in cloud provider config", func() {
		emptyConfig := map[string]string{"networkName": ""}
		configData, err := yaml.Marshal(emptyConfig)
		Expect(err).NotTo(HaveOccurred())

		configReader := strings.NewReader(string(configData))
		config, err := LoadCloudProviderConfig(configReader)
		Expect(err.Error()).To(Equal("networkName missing in cloud config"))
		Expect(config).To(BeNil())
	})

	It("should fail on empty clusterName in cloud provider config", func() {
		emptyConfig := map[string]string{"networkName": "my-network", "clusterName": ""}
		configData, err := yaml.Marshal(emptyConfig)
		Expect(err).NotTo(HaveOccurred())

		configReader := strings.NewReader(string(configData))
		config, err := LoadCloudProviderConfig(configReader)
		Expect(err.Error()).To(Equal("clusterName missing in cloud config"))
		Expect(config).To(BeNil())
	})
})
