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
	"os"
	"testing"
	"time"

	corev1alpha1 "github.com/onmetal/onmetal-api/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/onmetal/controller-utils/buildutils"
	"github.com/onmetal/controller-utils/modutils"
	computev1alpha1 "github.com/onmetal/onmetal-api/api/compute/v1alpha1"
	ipamv1alpha1 "github.com/onmetal/onmetal-api/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/api/networking/v1alpha1"
	storagev1alpha1 "github.com/onmetal/onmetal-api/api/storage/v1alpha1"
	envtestext "github.com/onmetal/onmetal-api/utils/envtest"
	"github.com/onmetal/onmetal-api/utils/envtest/apiserver"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"
)

var (
	cfg           *rest.Config
	k8sClient     client.Client
	testEnv       *envtest.Environment
	testEnvExt    *envtestext.EnvironmentExtensions
	cloudProvider cloudprovider.Interface
)

const (
	pollingInterval      = 50 * time.Millisecond
	eventuallyTimeout    = 10 * time.Second
	consistentlyDuration = 1 * time.Second
	apiServiceTimeout    = 5 * time.Minute
)

func TestAPIs(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)

	RegisterFailHandler(Fail)

	RunSpecs(t, "Cloud Provider Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{}
	testEnvExt = &envtestext.EnvironmentExtensions{
		APIServiceDirectoryPaths: []string{
			modutils.Dir("github.com/onmetal/onmetal-api", "config", "apiserver", "apiservice", "bases"),
		},
		ErrorIfAPIServicePathIsMissing: true,
	}

	cfg, err = envtestext.StartWithExtensions(testEnv, testEnvExt)
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	DeferCleanup(envtestext.StopWithExtensions, testEnv, testEnvExt)

	Expect(computev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(ipamv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(storagev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(networkingv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	// Init package-level k8sClient
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	SetClient(k8sClient)

	apiSrv, err := apiserver.New(cfg, apiserver.Options{
		MainPath:     "github.com/onmetal/onmetal-api/cmd/onmetal-apiserver",
		BuildOptions: []buildutils.BuildOption{buildutils.ModModeMod},
		ETCDServers:  []string{testEnv.ControlPlane.Etcd.URL.String()},
		Host:         testEnvExt.APIServiceInstallOptions.LocalServingHost,
		Port:         testEnvExt.APIServiceInstallOptions.LocalServingPort,
		CertDir:      testEnvExt.APIServiceInstallOptions.LocalServingCertDir,
	})
	Expect(err).NotTo(HaveOccurred())

	By("starting the onmetal-api aggregated api server")
	Expect(apiSrv.Start()).To(Succeed())
	DeferCleanup(apiSrv.Stop)

	Expect(envtestext.WaitUntilAPIServicesReadyWithTimeout(apiServiceTimeout, testEnvExt, k8sClient, scheme.Scheme)).To(Succeed())
})

func SetupTest(ctx context.Context) (*corev1.Namespace, *onmetalLoadBalancer, string) {

	var (
		ns          = &corev1.Namespace{}
		olb         = &onmetalLoadBalancer{}
		networkName = "my-network"
	)

	BeforeEach(func() {
		*ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"},
		}
		Expect(k8sClient.Create(ctx, ns)).NotTo(HaveOccurred(), "failed to create test namespace")
		DeferCleanup(k8sClient.Delete, ctx, ns)

		By("creating a machine class")
		machineClass := &computev1alpha1.MachineClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "machine-class",
			},
			Capabilities: corev1alpha1.ResourceList{
				corev1alpha1.ResourceCPU:    resource.MustParse("1"),
				corev1alpha1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
		Expect(k8sClient.Create(ctx, machineClass)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, machineClass)

		user, err := testEnv.AddUser(envtest.User{
			Name:   "dummy",
			Groups: []string{"system:authenticated", "system:masters"},
		}, nil)
		Expect(err).NotTo(HaveOccurred())

		kubeconfigData, err := user.KubeConfig()
		Expect(err).NotTo(HaveOccurred())

		clientConfig, err := clientcmd.Load(kubeconfigData)
		Expect(err).NotTo(HaveOccurred())
		clientConfig.Contexts[clientConfig.CurrentContext].Namespace = ns.Name

		namespacedKubeconfigData, err := clientcmd.Write(*clientConfig)
		Expect(err).NotTo(HaveOccurred())

		kubeconfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "kubeconfig")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = kubeconfigFile.Close()
		}()
		Expect(os.WriteFile(kubeconfigFile.Name(), namespacedKubeconfigData, 0666)).To(Succeed())

		curr := OnmetalKubeconfigPath
		defer func() {
			OnmetalKubeconfigPath = curr
		}()
		OnmetalKubeconfigPath = kubeconfigFile.Name()

		cloudConfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "cloud.yaml")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = cloudConfigFile.Close()
		}()
		cloudConfig := CloudConfig{NetworkName: networkName}
		cloudConfigData, err := yaml.Marshal(&cloudConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cloudConfigFile.Name(), cloudConfigData, 0666)).To(Succeed())

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		k8sClientSet, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		clientBuilder := clientbuilder.NewDynamicClientBuilder(testEnv.Config, k8sClientSet.CoreV1(), "default")
		cloudProvider, err = cloudprovider.InitCloudProvider(ProviderName, cloudConfigFile.Name())
		Expect(err).NotTo(HaveOccurred())
		cloudProvider.Initialize(clientBuilder, ctx.Done())

		newLB := newOnmetalLoadBalancer(k8sClient, k8sClient, ns.Name, networkName)
		*olb = *newLB.(*onmetalLoadBalancer)
	})
	return ns, olb, networkName
}
