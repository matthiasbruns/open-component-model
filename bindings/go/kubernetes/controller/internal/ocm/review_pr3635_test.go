package ocm_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Masterminds/semver/v3"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

var _ = Describe("ReviewPR3635 version selection", func() {
	It("characterizes empty-constraint selection pending the controller API decision", func(ctx SpecContext) {
		_, err := semver.NewConstraint("")
		Expect(err).To(HaveOccurred())
		got, err := ocm.GetLatestValidVersion(ctx, versioning.Default(), []string{"1.0.0", "2.0.0-rc.1", "zzz"}, "")
		// This records the reviewed behavior; legacy rejection is not an agreed requirement.
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("2.0.0-rc.1"))
		GinkgoWriter.Printf("OBSERVATION: legacy parser rejects empty; controller selects %q\n", got)
	})
	It("selects a stable version for an explicit wildcard constraint", func(ctx SpecContext) {
		stable, err := ocm.GetLatestValidVersion(ctx, versioning.Default(), []string{"1.0.0", "2.0.0-rc.1", "zzz"}, "*")
		Expect(err).NotTo(HaveOccurred())
		Expect(stable).To(Equal("1.0.0"))
	})
	It("honors regexp exclusion and preserves original version spelling", func(ctx SpecContext) {
		filter, err := ocm.RegexpFilter(`^v1\.`)
		Expect(err).NotTo(HaveOccurred())
		got, err := ocm.GetLatestValidVersion(ctx, versioning.Default(), []string{"v1.2.0+build.5", "2.0.0", "zzz"}, ">=1.0.0", filter)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("v1.2.0+build.5"))
	})
})
