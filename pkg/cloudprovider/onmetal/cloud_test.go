// Copyright 2023 OnMetal authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package onmetal

import (
	"github.com/onmetal/onmetal-api/utils/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cloud", func() {
	ctx := testing.SetupContext()
	SetupTest(ctx)

	It("should ensure the correct cloud provider setup", func() {
		Expect(cloudProvider.HasClusterID()).To(BeTrue())

		Expect(cloudProvider.ProviderName()).To(Equal("onmetal"))

		clusters, ok := cloudProvider.Clusters()
		Expect(clusters).To(BeNil())
		Expect(ok).To(BeFalse())

		instances, ok := cloudProvider.Instances()
		Expect(instances).To(BeNil())
		Expect(ok).To(BeFalse())

		zones, ok := cloudProvider.Zones()
		Expect(zones).To(BeNil())
		Expect(ok).To(BeFalse())

		routes, ok := cloudProvider.Routes()
		Expect(routes).To(BeNil())
		Expect(ok).To(BeFalse())
	})
})
