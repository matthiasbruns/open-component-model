package versioning_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestReviewPR3635_5d3e672f_MixedCaptureTransitivity(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(versioning.NewRegexScheme("mixed", regexp.MustCompile(`^(?P<value>[0-9a-z]+)$`), []string{"value"}))
	for _, pair := range [][2]string{{"2", "10"}, {"10", "1a"}, {"1a", "2"}} {
		c, err := reg.Compare(pair[0], pair[1])
		r.NoError(err)
		t.Logf("Compare(%q, %q) = %d", pair[0], pair[1], c)
	}
	for _, input := range [][]string{{"2", "10", "1a"}, {"10", "1a", "2"}, {"1a", "2", "10"}} {
		r.NoError(reg.SortDescending(input))
		t.Logf("sorted: %v", input)
	}
	ab, err := reg.Compare("2", "10")
	r.NoError(err)
	bc, err := reg.Compare("10", "1a")
	r.NoError(err)
	ac, err := reg.Compare("2", "1a")
	r.NoError(err)
	r.Negative(ab)
	r.Negative(bc)
	r.Negative(ac, "comparison must be transitive")
}

func TestReviewPR3635_5d3e672f_MissingOperand(t *testing.T) {
	for _, constraint := range []string{">=", ">=10 <", ">=10, !="} {
		t.Run(constraint, func(t *testing.T) {
			r := require.New(t)
			scheme, found := versioning.BuiltinScheme(versioning.BuiltinBuildNumber)
			r.True(found)
			reg := versioning.NewRegistry(scheme)
			validationErr := reg.ValidateConstraint(constraint)
			ok, err := reg.Satisfies("20", constraint)
			out, filterErr := reg.Filter([]string{"20"}, constraint)
			t.Logf("validation=%v satisfies=(%v,%v) filter=(%v,%v)", validationErr, ok, err, out, filterErr)
			r.Error(validationErr, "operator without operand must not validate")
		})
	}
}

func TestReviewPR3635_5d3e672f_ForeignConstraintWithSemverFallback(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(versioning.NewRegexScheme("build", regexp.MustCompile(`^build-(?P<n>\d+)$`), []string{"n"}), versioning.NewLooseSemverScheme())
	r.NoError(reg.ValidateConstraint(">=build-2"))
	out, err := reg.Filter([]string{"build-10", "1.0.0"}, ">=build-2")
	t.Logf("filter=(%v,%v)", out, err)
	ok, gateErr := reg.Satisfies("1.0.0", ">=build-2")
	t.Logf("gate=(%v,%v)", ok, gateErr)
	r.NoError(err, "valid foreign constraint should retain the semver history, like the reverse case")
	r.Equal([]string{"build-10", "1.0.0"}, out)
}
