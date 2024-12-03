// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

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
		sampleConfig := map[string]any{"networkName": "my-network", "prefixNames": []string{"my-prefix"}, "clusterName": "my-cluster"}
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
		Expect(config.cloudConfig.PrefixNames).To(Equal([]string{"my-prefix"}))
		Expect(config.cloudConfig.ClusterName).To(Equal("my-cluster"))
	})

	It("should get the default namespace if no namespace was defined for an auth context", func() {
		sampleConfig := map[string]any{"networkName": "my-network", "prefixNames": []string{"my-prefix"}, "clusterName": "my-cluster"}
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
		Expect(config.cloudConfig.PrefixNames).To(Equal([]string{"my-prefix"}))
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
