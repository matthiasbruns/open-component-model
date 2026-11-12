package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestReviewPR4206_F1_NestedSpecCaseVariantIsNotSilentlyDropped pins the behaviour of a
// nested configuration spec inside a document that was itself decoded leniently.
//
// .ocmconfig is read through sigs.k8s.io/yaml, i.e. encoding/json v1 semantics, so a member
// spelled with non-canonical casing survives into runtime.Raw.Data verbatim (Canonicalize
// reorders members but never changes their case). Scheme.Convert then decodes that Raw with a
// bare encoding/json/v2 Unmarshal, which matches member names case-sensitively and - because
// RejectUnknownMembers is not set - drops anything that does not match without reporting it.
//
// The result is a credential source that contributes nothing while Convert returns nil, so the
// failure surfaces much later as an anonymous pull or a 401. Either the nested spec is decoded
// with the leniency of the document it came from, or a dropped member is a hard error; silently
// discarding it is the one outcome a user cannot diagnose.
func TestReviewPR4206_F1_NestedSpecCaseVariantIsNotSilentlyDropped(t *testing.T) {
	for _, tt := range []struct {
		name string
		doc  string
	}{
		{
			name: "canonical casing",
			doc:  "type: DockerConfig/v1\ndockerConfigFile: ~/.docker/config.json\n",
		},
		{
			name: "case variant accepted by encoding/json v1",
			doc:  "type: DockerConfig/v1\nDockerConfigFile: ~/.docker/config.json\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			scheme := runtime.NewScheme()
			scheme.MustRegisterWithAlias(&DockerConfig{},
				runtime.NewVersionedType("DockerConfig", "v1"),
				runtime.NewUnversionedType("DockerConfig"),
			)

			// Outer document: decoded the way .ocmconfig is read.
			var raw runtime.Raw
			r.NoError(yaml.Unmarshal([]byte(tt.doc), &raw))

			// Nested spec: resolved through the scheme.
			var cfg DockerConfig
			err := scheme.Convert(&raw, &cfg)
			r.NoError(err)

			// Either the member is decoded, or Convert reports that it was not. Returning a
			// zero-valued credential source with a nil error is what loses the credentials.
			r.Equal("~/.docker/config.json", cfg.DockerConfigFile,
				"member silently dropped from %s: credential source lost without an error", string(raw.Data))
		})
	}
}
