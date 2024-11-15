// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	"github.com/ironcore-dev/controller-utils/buildutils"
	"github.com/ironcore-dev/controller-utils/modutils"
	commonv1alpha1 "github.com/ironcore-dev/ironcore/api/common/v1alpha1"
	computev1alpha1 "github.com/ironcore-dev/ironcore/api/compute/v1alpha1"
	corev1alpha1 "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	ipamv1alpha1 "github.com/ironcore-dev/ironcore/api/ipam/v1alpha1"
	networkingv1alpha1 "github.com/ironcore-dev/ironcore/api/networking/v1alpha1"
	storagev1alpha1 "github.com/ironcore-dev/ironcore/api/storage/v1alpha1"
	envtestext "github.com/ironcore-dev/ironcore/utils/envtest"
	"github.com/ironcore-dev/ironcore/utils/envtest/apiserver"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	testEnvExt *envtestext.EnvironmentExtensions
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
	testEnv = &envtest.Environment{
		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("1.30.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}
	testEnvExt = &envtestext.EnvironmentExtensions{
		APIServiceDirectoryPaths: []string{
			modutils.Dir("github.com/ironcore-dev/ironcore", "config", "apiserver", "apiservice", "bases"),
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
		MainPath:     "github.com/ironcore-dev/ironcore/cmd/ironcore-apiserver",
		BuildOptions: []buildutils.BuildOption{buildutils.ModModeMod},
		ETCDServers:  []string{testEnv.ControlPlane.Etcd.URL.String()},
		Host:         testEnvExt.APIServiceInstallOptions.LocalServingHost,
		Port:         testEnvExt.APIServiceInstallOptions.LocalServingPort,
		CertDir:      testEnvExt.APIServiceInstallOptions.LocalServingCertDir,
	})
	Expect(err).NotTo(HaveOccurred())

	By("starting the ironcore aggregated server")
	Expect(apiSrv.Start()).To(Succeed())
	DeferCleanup(apiSrv.Stop)

	Expect(envtestext.WaitUntilAPIServicesReadyWithTimeout(apiServiceTimeout, testEnvExt, k8sClient, scheme.Scheme)).To(Succeed())
})

func SetupTest() (*corev1.Namespace, *cloudprovider.Interface, *networkingv1alpha1.Network, string) {
	var (
		ns          = &corev1.Namespace{}
		cp          cloudprovider.Interface
		network     = &networkingv1alpha1.Network{}
		clusterName = "test"
	)

	BeforeEach(func(ctx SpecContext) {
		*ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"},
		}
		Expect(k8sClient.Create(ctx, ns)).NotTo(HaveOccurred(), "failed to create test namespace")
		DeferCleanup(k8sClient.Delete, ns)

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
		DeferCleanup(k8sClient.Delete, machineClass)

		By("creating network")
		*network = networkingv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "network-",
			},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())

		By("creating prefix")
		prefix := &ipamv1alpha1.Prefix{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "prefix-",
				Namespace:    ns.Name,
			},
			Spec: ipamv1alpha1.PrefixSpec{
				IPFamily: corev1.IPv4Protocol,
				Prefix:   commonv1alpha1.MustParseNewIPPrefix("100.0.0.0/16"),
			},
		}
		Expect(k8sClient.Create(ctx, prefix)).To(Succeed())
		DeferCleanup(k8sClient.Delete, prefix)

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

		curr := IroncoreKubeconfigPath
		defer func() {
			IroncoreKubeconfigPath = curr
		}()
		IroncoreKubeconfigPath = kubeconfigFile.Name()

		cloudConfigFile, err := os.CreateTemp(GinkgoT().TempDir(), "cloud.yaml")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = cloudConfigFile.Close()
		}()
		cloudConfig := CloudConfig{NetworkName: network.Name, PrefixName: prefix.Name, ClusterName: clusterName}
		cloudConfigData, err := yaml.Marshal(&cloudConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(cloudConfigFile.Name(), cloudConfigData, 0666)).To(Succeed())

		cloudProviderCtx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		k8sClientSet, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		clientBuilder := clientbuilder.NewDynamicClientBuilder(testEnv.Config, k8sClientSet.CoreV1(), "default")
		cp, err = cloudprovider.InitCloudProvider(ProviderName, cloudConfigFile.Name())
		Expect(err).NotTo(HaveOccurred())
		cp.Initialize(clientBuilder, cloudProviderCtx.Done())
	})

	return ns, &cp, network, clusterName
}
