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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onmetal/onmetal-api/testutils"
)

var _ = Describe("LoadBalancer", func() {
	ctx := testutils.SetupContext()
	ns := SetupTest(ctx)

	It("Should get LoadBalancer name", func() {

		By("getting the LoadBalancer interface")
		lb, ok := provider.LoadBalancer()
		Expect(ok).To(BeTrue())

		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-svc",
				Namespace: ns.Name,
			},
		}

		By("getting name of LoadBalancer")
		Eventually(func(g Gomega) {
			name := lb.GetLoadBalancerName(ctx, "", service)
			g.Expect(name).To(Equal(ns.Name + "_" + service.Name))
		}).Should(Succeed())
	})
})
