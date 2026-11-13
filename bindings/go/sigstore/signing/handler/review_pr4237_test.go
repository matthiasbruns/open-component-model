package handler_test

// Review evidence for PR #4237. Runs the real handler against the pinned OCM
// cosign release and a keyless bundle recorded by PR #4236's replay suite
// (testdata/review_pr4237). The bundle's Fulcio leaf certificate expired on
// 2026-11-12T15:58:30Z and the bundle carries an RFC3161 timestamp from the
// scaffolding TSA, so it exercises exactly the aged-bundle path the PR fixes.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	trustedrootv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot/v1alpha1"
)

const reviewPR4237Dir = "testdata/review_pr4237"

type reviewPR4237Meta struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	DigestValue            string `json:"digestValue"`
	Algorithm              string `json:"algorithm"`
	MediaType              string `json:"mediaType"`
	OIDCIssuer             string `json:"oidcIssuer"`
	OIDCIdentity           string `json:"oidcIdentity"`
}

func reviewPR4237Load(t *testing.T) (reviewPR4237Meta, map[string]any, map[string]any) {
	t.Helper()
	r := require.New(t)
	var m reviewPR4237Meta
	raw, err := os.ReadFile(filepath.Join(reviewPR4237Dir, "meta.json"))
	r.NoError(err)
	r.NoError(json.Unmarshal(raw, &m))
	var bundle, root map[string]any
	raw, err = os.ReadFile(filepath.Join(reviewPR4237Dir, "bundle.json"))
	r.NoError(err)
	r.NoError(json.Unmarshal(raw, &bundle))
	raw, err = os.ReadFile(filepath.Join(reviewPR4237Dir, "trusted_root.json"))
	r.NoError(err)
	r.NoError(json.Unmarshal(raw, &root))
	return m, bundle, root
}

// reviewPR4237Handler forces the pinned, checksum-verified cosign download
// (bindings/go/sigstore/signing/handler/internal/.env) instead of whatever is on PATH.
func reviewPR4237Handler() *handler.Handler {
	return handler.New(handler.WithLookPath(func(string) (string, error) { return "", exec.ErrNotFound }))
}

func TestReviewPR4237_V1_AgedBundleWithSignedTimestamp(t *testing.T) {
	m, _, _ := reviewPR4237Load(t)

	tests := []struct {
		name          string
		private       bool
		identity      string
		digest        string
		mutateBundle  func(b map[string]any)
		mutateRoot    func(r map[string]any)
		wantErr       bool
		wantErrSubstr string
	}{
		{
			// The fix: aged keyless bundle with a TSA timestamp verifies offline.
			name:    "private infrastructure, aged bundle with TSA timestamp verifies",
			private: true,
		},
		{
			// Control: public path (tlog checked) already verifies via the Rekor integrated time.
			name:    "tlog path, aged bundle verifies via integrated time",
			private: false,
		},
		{
			// No downgrade: without a signed timestamp the expired leaf is still rejected.
			name:          "private infrastructure, timestamp stripped is rejected",
			private:       true,
			mutateBundle:  func(b map[string]any) { delete(vm(b), "timestampVerificationData") },
			wantErr:       true,
			wantErrSubstr: "leaf certificate verification failed",
		},
		{
			// Fail closed: a forged timestamp must not be accepted.
			name:    "private infrastructure, forged timestamp is rejected",
			private: true,
			mutateBundle: func(b map[string]any) {
				vm(b)["timestampVerificationData"] = map[string]any{
					"rfc3161Timestamps": []any{map[string]any{"signedTimestamp": base64.StdEncoding.EncodeToString([]byte("forged"))}},
				}
			},
			wantErr: true,
		},
		{
			// Fail closed: an empty timestamp entry (bundleHasSignedTimestamp returns true for it) must not be accepted.
			name:    "private infrastructure, empty timestamp entry is rejected",
			private: true,
			mutateBundle: func(b map[string]any) {
				vm(b)["timestampVerificationData"] = map[string]any{"rfc3161Timestamps": []any{map[string]any{}}}
			},
			wantErr: true,
		},
		{
			// Fail closed: an empty signedTimestamp value must not be accepted.
			name:    "private infrastructure, empty signedTimestamp is rejected",
			private: true,
			mutateBundle: func(b map[string]any) {
				vm(b)["timestampVerificationData"] = map[string]any{"rfc3161Timestamps": []any{map[string]any{"signedTimestamp": ""}}}
			},
			wantErr: true,
		},
		{
			// Fail closed: a timestamp whose TSA is not in the trusted root must not be accepted.
			name:       "private infrastructure, TSA missing from trusted root is rejected",
			private:    true,
			mutateRoot: func(r map[string]any) { delete(r, "timestampAuthorities") },
			wantErr:    true,
		},
		{
			// Identity policy still enforced alongside --use-signed-timestamps.
			name:     "private infrastructure, wrong identity is rejected",
			private:  true,
			identity: "https://kubernetes.io/namespaces/evil/serviceaccounts/default",
			wantErr:  true,
		},
		{
			// Signature still bound to the digest alongside --use-signed-timestamps.
			name:    "private infrastructure, wrong digest is rejected",
			private: true,
			digest:  "0000000000000000000000000000000000000000000000000000000000000000",
			wantErr: true,
		},
	}

	h := reviewPR4237Handler()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			_, bundle, root := reviewPR4237Load(t)
			if tc.mutateBundle != nil {
				tc.mutateBundle(bundle)
			}
			if tc.mutateRoot != nil {
				tc.mutateRoot(root)
			}
			bundleJSON, err := json.Marshal(bundle)
			r.NoError(err)
			rootJSON, err := json.Marshal(root)
			r.NoError(err)

			identity := m.OIDCIdentity
			if tc.identity != "" {
				identity = tc.identity
			}
			digest := m.DigestValue
			if tc.digest != "" {
				digest = tc.digest
			}
			cfg := &v1alpha1.VerifyConfig{
				CertificateOIDCIssuer: m.OIDCIssuer,
				CertificateIdentity:   identity,
				PrivateInfrastructure: tc.private,
			}
			cfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))

			signed := descruntime.Signature{
				Name: "replay",
				Digest: descruntime.Digest{
					HashAlgorithm:          m.HashAlgorithm,
					NormalisationAlgorithm: m.NormalisationAlgorithm,
					Value:                  digest,
				},
				Signature: descruntime.SignatureInfo{
					Algorithm: m.Algorithm,
					MediaType: m.MediaType,
					Value:     base64.StdEncoding.EncodeToString(bundleJSON),
				},
			}

			err = h.Verify(t.Context(), signed, cfg, &trustedrootv1.TrustedRoot{TrustedRootJSON: string(rootJSON)})
			if tc.wantErr {
				r.Error(err)
				t.Logf("rejected: %v", err)
				if tc.wantErrSubstr != "" {
					r.ErrorContains(err, tc.wantErrSubstr)
				}
				return
			}
			r.NoError(err)
		})
	}
}

func vm(b map[string]any) map[string]any {
	return b["verificationMaterial"].(map[string]any)
}
