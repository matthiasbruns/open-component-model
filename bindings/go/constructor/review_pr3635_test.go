package constructor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestReviewPR3635_ElementVersionUploadSideEffects(t *testing.T) {
	for _, kind := range []string{"resources", "sources"} {
		for _, version := range []string{"not-a-version", "1.0.0"} {
			t.Run(kind+"/"+version, func(t *testing.T) {
				r := require.New(t)
				data := fmt.Sprintf(`components:
- name: acme.org/review
  version: 1.0.0
  provider:
    name: acme
  %s:
  - name: artifact
    version: %s
    type: blob
    input:
      type: mock/v1
`, kind, version)
				var spec constructorv1.ComponentConstructor
				r.NoError(yaml.Unmarshal([]byte(data), &spec))
				repo := newMockTargetRepository()
				opts := Options{
					TargetRepositoryProvider: &mockTargetRepositoryProvider{repo: repo},
					ResourceInputMethodProvider: &mockInputMethodProvider{methods: map[runtime.Type]ResourceInputMethod{
						runtime.NewVersionedType("mock", "v1"): &mockInputMethod{processedBlob: inmemory.New(strings.NewReader("review content"))},
					}},
					SourceInputMethodProvider: &mockSourceInputMethodProvider{methods: map[runtime.Type]SourceInputMethod{
						runtime.NewVersionedType("mock", "v1"): &mockSourceInputMethod{processedBlob: inmemory.New(strings.NewReader("review content"))},
					}},
				}
				err := NewDefaultConstructor(constructorruntime.ConvertToRuntimeConstructor(&spec), opts).Construct(t.Context())
				if version == "1.0.0" {
					r.NoError(err)
					r.Len(repo.addedVersions, 1)
					r.Equal(1, len(repo.addedLocalResources)+len(repo.addedSources))
					return
				}
				r.ErrorContains(err, `invalid version "not-a-version"`)
				r.Empty(repo.addedVersions)
				// Post-processing validation is intentional; no no-upload contract was established.
				t.Logf("OBSERVATION: rejected %s version after %d resource and %d source uploads", kind, len(repo.addedLocalResources), len(repo.addedSources))
			})
		}
	}
}

func TestReviewPR3635_CustomVersionAndSourceDefaulting(t *testing.T) {
	for _, sourceVersion := range []string{"", "2024-03-14"} {
		t.Run(sourceVersion, func(t *testing.T) {
			r := require.New(t)
			var spec constructorv1.ComponentConstructor
			sourceVersionField := ""
			if sourceVersion != "" {
				sourceVersionField = "version: " + sourceVersion
			}
			r.NoError(yaml.Unmarshal([]byte(fmt.Sprintf(`components:
- name: acme.org/review
  version: 2024-03-15
  provider:
    name: acme
  sources:
  - name: source
    %s
    type: git
    access:
      type: review
`, sourceVersionField)), &spec))
			scheme, ok := versioning.BuiltinScheme(versioning.BuiltinAWSDate)
			r.True(ok)
			repo := newMockTargetRepository()
			opts := Options{TargetRepositoryProvider: &mockTargetRepositoryProvider{repo: repo}, VersioningRegistry: versioning.NewRegistry(scheme)}
			r.NoError(NewDefaultConstructor(constructorruntime.ConvertToRuntimeConstructor(&spec), opts).Construct(t.Context()))
			r.Len(repo.addedVersions, 1)
			want := sourceVersion
			if want == "" {
				want = "2024-03-15"
			}
			r.Equal(want, repo.addedVersions[0].Component.Sources[0].Version)
		})
	}
}
