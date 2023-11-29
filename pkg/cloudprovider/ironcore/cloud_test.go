// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ironcore

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cloud", func() {
	_, cp, _, _ := SetupTest()

	It("should ensure the correct cloud provider setup", func() {
		Expect((*cp).HasClusterID()).To(BeTrue())

		Expect((*cp).ProviderName()).To(Equal("ironcore"))

		clusters, ok := (*cp).Clusters()
		Expect(clusters).To(BeNil())
		Expect(ok).To(BeFalse())

		instances, ok := (*cp).Instances()
		Expect(instances).To(BeNil())
		Expect(ok).To(BeFalse())

		zones, ok := (*cp).Zones()
		Expect(zones).To(BeNil())
		Expect(ok).To(BeFalse())
	})
})
