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
	"context"
	"go/build"
	"io/ioutil"
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
	"golang.org/x/mod/modfile"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
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
	machinePackagePath := reflect.TypeOf(computev1alpha1.Machine{}).PkgPath()

	goModData, err := ioutil.ReadFile(filepath.Join("..", "..", "..", "go.mod"))
	Expect(err).NotTo(HaveOccurred())

	goModFile, err := modfile.Parse("", goModData, nil)
	Expect(err).NotTo(HaveOccurred())

	packagePaths := []string{
		machinePackagePath,
	}

	modulePaths := make([]string, len(packagePaths))

	for _, req := range goModFile.Require {
		for i, packagePath := range packagePaths {
			if strings.HasPrefix(packagePath, req.Mod.Path) {
				modulePaths[i] = req.Mod.String()
			}
		}
	}

	Expect(modulePaths).NotTo(ContainElement(""))

	crdPaths := make([]string, len(modulePaths))

	for i, modulePath := range modulePaths {
		crdPaths[i] = filepath.Join(build.Default.GOPATH, "pkg", "mod", modulePath, "config", "apiserver", "apiservice", "bases")
	}

	testEnv = &envtest.Environment{}
	testEnvExt = &envtestutils.EnvironmentExtensions{
		APIServiceDirectoryPaths:       crdPaths,
		ErrorIfAPIServicePathIsMissing: true,
	}

	cfg, err = envtestutils.StartWithExtensions(testEnv, testEnvExt)
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	DeferCleanup(envtestutils.StopWithExtensions, testEnv, testEnvExt)

	Expect(computev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(storagev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(ipamv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(networkingv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	apiSrv, err := apiserver.New(cfg, apiserver.Options{
		Command:     []string{"go", "run", filepath.Join(build.Default.GOPATH, "pkg", "mod", modulePaths[0], "cmd", "apiserver", "main.go")},
		ETCDServers: []string{testEnv.ControlPlane.Etcd.URL.String()},
		Host:        testEnvExt.APIServiceInstallOptions.LocalServingHost,
		Port:        testEnvExt.APIServiceInstallOptions.LocalServingPort,
		CertDir:     testEnvExt.APIServiceInstallOptions.LocalServingCertDir,
	})
	Expect(err).NotTo(HaveOccurred())
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go func() {
		defer GinkgoRecover()
		_ = apiSrv.Start(ctx)
	}()

	err = envtestutils.WaitUntilAPIServicesReadyWithTimeout(5*time.Minute, testEnvExt, k8sClient, scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
})
