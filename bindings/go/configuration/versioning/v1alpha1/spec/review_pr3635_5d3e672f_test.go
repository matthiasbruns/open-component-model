package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	versioningspec "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
)

func TestReviewPR3635_5d3e672f_DuplicateCaptureNames(t *testing.T) {
	r := require.New(t)
	cfg := &versioningspec.Config{Schemes: []versioningspec.VersionScheme{{
		Name:             "alternative-prefixes",
		Pattern:          `^(?:v(?P<n>\d+)|r(?P<n>\d+))$`,
		ComparisonGroups: []string{"n"},
	}}}
	reg, err := cfg.Registry()
	if err != nil {
		message := strings.ToLower(err.Error())
		r.Regexp(`duplicate|repeated|unique`, message)
		r.Regexp(`capture|group|name`, message)
		t.Logf("duplicate capture names explicitly rejected: %v", err)
		return
	}
	r.True(reg.Valid("v2"))
	r.True(reg.Valid("v10"))
	c, err := reg.Compare("v2", "v10")
	r.NoError(err)
	ok, err := reg.Satisfies("v2", ">=v10")
	r.NoError(err)
	t.Logf("Compare(v2,v10)=%d; Satisfies(v2,>=v10)=%v", c, ok)
	t.Run("numeric comparison", func(t *testing.T) {
		r := require.New(t)
		r.Negative(c, "active numeric capture must determine comparison; alternatively reject duplicate names at config load")
	})
	t.Run("constraint", func(t *testing.T) {
		r := require.New(t)
		r.False(ok, "v2 must not satisfy >=v10")
	})
}
