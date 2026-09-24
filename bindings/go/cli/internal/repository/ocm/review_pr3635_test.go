package ocm

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestReviewPR3635MixedSchemeFilter(t *testing.T) {
	for _, tc := range []struct {
		name       string
		versions   []string
		constraint string
		want       []string
	}{
		{"custom-only", []string{"build-99", "build-100"}, ">=build-100", []string{"build-100"}},
		{"mixed-semver-control", []string{"build-99", "build-100", "1.0.0", "2.0.0"}, ">=2.0.0", []string{"build-99", "build-100", "2.0.0"}},
		{"mixed-custom-regression", []string{"build-99", "build-100", "1.0.0", "2.0.0"}, ">=build-100", []string{"build-100", "1.0.0", "2.0.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := versioning.NewRegistry(versioning.NewRegexScheme("build", regexp.MustCompile(`^build-(?P<n>\d+)$`), []string{"n"}), versioning.NewLooseSemverScheme())
			r.NoError(reg.ValidateConstraint(tc.constraint))
			repo := newMockComponentVersionRepository()
			for _, version := range tc.versions {
				r.NoError(repo.AddComponentVersion(t.Context(), makeDescriptor("acme.org/service", version)))
			}
			t.Run("transfer-filter", func(t *testing.T) {
				r := require.New(t)
				versions, err := VersionsWithFiltering(t.Context(), "acme.org/service", repo, VersionOptions{SemverConstraint: tc.constraint, Registry: reg})
				r.NoError(err, "valid custom constraint should retain versions whose scheme cannot apply it")
				r.ElementsMatch(tc.want, versions)
			})
			t.Run("listing", func(t *testing.T) {
				r := require.New(t)
				descs, err := ListComponentVersions(t.Context(), repo, WithComponentNames([]string{"acme.org/service"}), WithSemverConstraint(tc.constraint), WithVersioningRegistry(reg))
				r.NoError(err)
				var versions []string
				for _, desc := range descs {
					versions = append(versions, desc.Component.Version)
				}
				r.ElementsMatch(tc.want, versions)
			})
		})
	}
}

func TestReviewPR3635MalformedFilter(t *testing.T) {
	r := require.New(t)
	scheme, ok := versioning.BuiltinScheme(versioning.BuiltinCalVerFull)
	r.True(ok)
	reg := versioning.NewRegistry(scheme)
	for _, tc := range []struct {
		constraint string
		malformed  bool
		want       []string
	}{
		{"", false, []string{"2024.03.14", "2024.03.15", "2024.10.01"}},
		{">=2024.03.15", false, []string{"2024.03.15", "2024.10.01"}},
		{"<2024.10.01", false, []string{"2024.03.14", "2024.03.15"}},
		{">=2024.03.15 <2024.10.01", false, []string{"2024.03.15"}},
		{">=", true, nil},
		{"<", true, nil},
		{">=2024.03.15 <", true, nil},
	} {
		t.Run(tc.constraint, func(t *testing.T) {
			r := require.New(t)
			repo := newMockComponentVersionRepository()
			for _, version := range []string{"2024.03.14", "2024.03.15", "2024.10.01"} {
				r.NoError(repo.AddComponentVersion(t.Context(), makeDescriptor("acme.org/service", version)))
			}
			t.Run("validation", func(t *testing.T) {
				r := require.New(t)
				err := reg.ValidateConstraint(tc.constraint)
				if tc.malformed {
					r.Error(err, "missing relational operand must be rejected")
				} else {
					r.NoError(err)
				}
			})
			t.Run("transfer-filter", func(t *testing.T) {
				r := require.New(t)
				versions, err := VersionsWithFiltering(t.Context(), "acme.org/service", repo, VersionOptions{SemverConstraint: tc.constraint, Registry: reg})
				t.Logf("VersionsWithFiltering: versions=%v err=%v", versions, err)
				if tc.malformed {
					r.Error(err, "malformed relational filter must not select versions")
				} else {
					r.NoError(err)
					r.ElementsMatch(tc.want, versions)
				}
			})
			t.Run("listing", func(t *testing.T) {
				r := require.New(t)
				descs, err := ListComponentVersions(t.Context(), repo, WithComponentNames([]string{"acme.org/service"}), WithSemverConstraint(tc.constraint), WithVersioningRegistry(reg))
				t.Logf("ListComponentVersions: count=%d err=%v", len(descs), err)
				if tc.malformed {
					r.Error(err, "malformed relational filter must not select versions")
				} else {
					r.NoError(err)
					var versions []string
					for _, desc := range descs {
						versions = append(versions, desc.Component.Version)
					}
					r.ElementsMatch(tc.want, versions)
				}
			})
		})
	}
}
