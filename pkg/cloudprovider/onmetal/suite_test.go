/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onmetal

import (
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	computev1alpha1 "github.com/onmetal/onmetal-api/apis/compute/v1alpha1"
	ipamv1alpha1 "github.com/onmetal/onmetal-api/apis/ipam/v1alpha1"
	networkingv1alpha1 "github.com/onmetal/onmetal-api/apis/networking/v1alpha1"
	storagev1alpha1 "github.com/onmetal/onmetal-api/apis/storage/v1alpha1"
	"github.com/onmetal/onmetal-api/envtestutils"
	"github.com/onmetal/onmetal-api/envtestutils/apiserver"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"golang.org/x/mod/modfile"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	testEnvExt *envtestutils.EnvironmentExtensions
)

func TestAPIs(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(100 * time.Millisecond)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultEventuallyTimeout(30 * time.Second)
	SetDefaultConsistentlyDuration(30 * time.Second)
	RegisterFailHandler(Fail)

	RunSpecs(t, "Cloud Provider Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("Bootstrapping test environment")
	onmetalApiPackagePath := reflect.TypeOf(computev1alpha1.Machine{}).PkgPath()
	clusterApiPackagePath := reflect.TypeOf(capiv1beta1.Cluster{}).PkgPath()

	goModData, err := os.ReadFile(filepath.Join("..", "..", "..", "go.mod"))
	Expect(err).NotTo(HaveOccurred())

	goModFile, err := modfile.Parse("", goModData, nil)
	Expect(err).NotTo(HaveOccurred())

	onmetalApiModulePath := findPackagePath(goModFile, onmetalApiPackagePath)
	Expect(onmetalApiModulePath).NotTo(Equal(""))
	clusterApiModulePath := findPackagePath(goModFile, clusterApiPackagePath)
	Expect(clusterApiModulePath).NotTo(Equal(""))

	onmetalApiCrdPath := filepath.Join(build.Default.GOPATH, "pkg", "mod", onmetalApiModulePath, "config", "apiserver", "apiservice", "bases")
	clusterApiCrdPath := filepath.Join(build.Default.GOPATH, "pkg", "mod", clusterApiModulePath, "config", "crd", "bases")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases"), clusterApiCrdPath},
	}
	testEnvExt = &envtestutils.EnvironmentExtensions{
		APIServiceDirectoryPaths:       []string{onmetalApiCrdPath},
		ErrorIfAPIServicePathIsMissing: true,
	}

	Expect(computev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(storagev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(ipamv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(networkingv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(capiv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())

	cfg, err = envtestutils.StartWithExtensions(testEnv, testEnvExt)
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	DeferCleanup(envtestutils.StopWithExtensions, testEnv, testEnvExt)

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	testbinPath := filepath.Join("..", "..", "..", "testbin")
	Expect(os.MkdirAll(testbinPath, os.ModePerm)).To(Succeed())

	apiSrvBinPath := filepath.Join("..", "..", "..", "testbin", "apiserver")
	absApiSrvBinPath, err := filepath.Abs(apiSrvBinPath)
	Expect(err).NotTo(HaveOccurred())

	if _, err := os.Stat(apiSrvBinPath); errors.Is(err, os.ErrNotExist) {
		cmd := exec.Command("go", "build", "-o",
			absApiSrvBinPath,
			filepath.Join(build.Default.GOPATH, "pkg", "mod", onmetalApiModulePath, "cmd", "apiserver", "main.go"),
		)
		cmd.Dir = filepath.Join(build.Default.GOPATH, "pkg", "mod", onmetalApiModulePath)
		Expect(cmd.Run()).To(Succeed())
	}

	apiSrv, err := apiserver.New(cfg, apiserver.Options{
		Command:      []string{apiSrvBinPath},
		ETCDServers:  []string{testEnv.ControlPlane.Etcd.URL.String()},
		Host:         testEnvExt.APIServiceInstallOptions.LocalServingHost,
		Port:         testEnvExt.APIServiceInstallOptions.LocalServingPort,
		CertDir:      testEnvExt.APIServiceInstallOptions.LocalServingCertDir,
		AttachOutput: true,
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(apiSrv.Start()).To(Succeed())
	DeferCleanup(apiSrv.Stop)

	err = envtestutils.WaitUntilAPIServicesReadyWithTimeout(5*time.Minute, testEnvExt, k8sClient, scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
})

func findPackagePath(modFile *modfile.File, packagePath string) string {
	for _, req := range modFile.Require {
		if strings.HasPrefix(packagePath, req.Mod.Path) {
			return req.Mod.String()
		}
	}
	return ""
}
