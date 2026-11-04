package v1alpha1_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicache "ocm.software/open-component-model/bindings/go/oci/cache"
	cachespec "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
	transferspec "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Review evidence for https://github.com/open-component-model/open-component-model/pull/4147
//
// LookupConfig decodes each entry with Scheme.Convert, which silently drops any
// field the Config struct does not declare. A misspelled remotePolicy key is
// therefore dropped, LookupConfig succeeds, and the effective policy falls back
// to the permissive IfNotPresent default with no error and no log line.
//
// This is NOT specific to PR #4147: eight of the nine OCM config types decode
// this way and transfer.config.ocm.software is the only one using
// runtime.DecodeStrict. TestReviewPR4147_ConfigDecodeLeniencyAcrossTypes
// records that spread for the sibling types; the two tests above cover the
// type this PR adds. The finding belongs to the config framework, not to this
// PR, and is filed as evidence for a separate issue rather than as a blocker.

func reviewDecodeGeneric(t *testing.T, y string) *genericv1.Config {
	t.Helper()
	var cfg genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(y), &cfg))
	return &cfg
}

func reviewCacheConfigYAML(key, value string) string {
	return fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    %s: %s
`, key, value)
}

// TestReviewPR4147_M1_MisspelledRemotePolicyMustNotBeSilentlyDropped asserts the
// behaviour we want: asking for the hardened policy under a key the struct does
// not declare must fail loudly instead of quietly yielding IfNotPresent.
//
// On PR head fc2336733b the "remote-policy", "remotePolicly", "remote_policy"
// and "policy" cases FAIL: LookupConfig returns no error and an empty
// RemotePolicy. Switching the Scheme.Convert call in LookupConfig to
// runtime.DecodeStrict (as transfer/v1alpha1/spec/config.go already does) makes
// the whole table pass.
func TestReviewPR4147_M1_MisspelledRemotePolicyMustNotBeSilentlyDropped(t *testing.T) {
	// An empty RemotePolicy is defaulted to IfNotPresent by
	// cache.Options.applyDefaults, so a dropped field is a silent downgrade
	// rather than a no-op.
	require.Equal(t, ocicache.RemotePolicyIfNotPresent, ocicache.Defaults().RemotePolicy,
		"precondition: the cache defaults an empty policy to IfNotPresent")

	tests := []struct {
		name string
		key  string
		// matchedByJSON is true for keys encoding/json still matches
		// case-insensitively, so they correctly yield Always.
		matchedByJSON bool
	}{
		{name: "correct key", key: "remotePolicy", matchedByJSON: true},
		{name: "all lowercase is matched case-insensitively", key: "remotepolicy", matchedByJSON: true},
		{name: "kebab case", key: "remote-policy"},
		{name: "transposed letters", key: "remotePolicly"},
		{name: "snake case", key: "remote_policy"},
		{name: "shortened", key: "policy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			cfg, err := cachespec.LookupConfig(reviewDecodeGeneric(t, reviewCacheConfigYAML(tt.key, "Always")))

			if tt.matchedByJSON {
				r.NoError(err)
				r.NotNil(cfg)
				r.Equal(cachespec.RemotePolicyAlways, cfg.RemotePolicy)

				return
			}

			// The operator asked for Always. Either the unknown key is
			// rejected, or the policy must still be Always. Silently
			// returning the permissive default is the defect.
			if err != nil {
				r.ErrorContains(err, tt.key, "the error should name the offending field")

				return
			}

			r.NotNil(cfg)
			r.Equalf(cachespec.RemotePolicyAlways, cfg.RemotePolicy,
				"key %q was dropped silently: LookupConfig returned no error and an empty "+
					"RemotePolicy, which the cache defaults to %q",
				tt.key, ocicache.RemotePolicyIfNotPresent)
		})
	}
}

// TestReviewPR4147_UnknownFieldMustNotBeSilentlyDropped is the minimal form of
// the same defect: an entry with a correct remotePolicy plus one field the
// struct does not declare. It FAILS on PR head fc2336733b.
func TestReviewPR4147_UnknownFieldMustNotBeSilentlyDropped(t *testing.T) {
	r := require.New(t)

	cfg := reviewDecodeGeneric(t, `type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: Always
    bogusField: 1
`)

	_, err := cachespec.LookupConfig(cfg)
	r.Error(err, "an undeclared field must be reported, not dropped")
	r.ErrorContains(err, "bogusField")
}

// TestReviewPR4147_ConfigDecodeLeniencyAcrossTypes records which OCM config
// types reject an undeclared field. It documents the current spread rather than
// asserting a fix, so it PASSES on PR head fc2336733b and is the evidence for
// treating the leniency as a framework-wide issue.
func TestReviewPR4147_ConfigDecodeLeniencyAcrossTypes(t *testing.T) {
	tests := []struct {
		name        string
		configType  string
		lookup      func(*genericv1.Config) error
		wantRejects bool
	}{
		{
			name:        "http.config.ocm.software",
			configType:  "http.config.ocm.software",
			lookup:      func(c *genericv1.Config) error { _, err := httpv1alpha1.LookupConfig(c); return err },
			wantRejects: false,
		},
		{
			name:        "checksum.http.config.ocm.software",
			configType:  "checksum.http.config.ocm.software",
			lookup:      func(c *genericv1.Config) error { _, err := checksumhttpv1alpha1.LookupConfig(c); return err },
			wantRejects: false,
		},
		{
			name:        "filesystem.config.ocm.software",
			configType:  "filesystem.config.ocm.software/v1alpha1", // only the versioned form is registered
			lookup:      func(c *genericv1.Config) error { _, err := filesystemv1alpha1.LookupConfig(c); return err },
			wantRejects: false,
		},
		{
			name:        "transfer.config.ocm.software (uses runtime.DecodeStrict)",
			configType:  "transfer.config.ocm.software",
			lookup:      func(c *genericv1.Config) error { _, err := transferspec.LookupConfig(c); return err },
			wantRejects: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			cfg := reviewDecodeGeneric(t, fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
  - type: %s
    bogusField: 1
`, tt.configType))

			err := tt.lookup(cfg)
			if tt.wantRejects {
				r.Error(err, "%s should reject an undeclared field", tt.configType)
				r.ErrorContains(err, "bogusField")

				return
			}
			r.NoError(err, "%s currently accepts an undeclared field", tt.configType)
		})
	}
}
