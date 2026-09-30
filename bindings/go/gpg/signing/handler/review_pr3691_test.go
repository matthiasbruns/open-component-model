package handler

import (
	"crypto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Test_Integration_ReviewPR3691_F1_RevokedPinnedKeyWithCosigner shows that a signature by a revoked
// pinned key is accepted once any other trusted key adds a signature: parseVerifyStatus takes GOODSIG
// from one signature and VALIDSIG (the fingerprint that is matched against the pin) from another.
func Test_Integration_ReviewPR3691_F1_RevokedPinnedKeyWithCosigner(t *testing.T) {
	_, err := exec.LookPath("gpg")
	require.NoError(t, err, "GnuPG >= 2.2 must be on PATH")

	h := mustHandler(t)
	digest := makeDigest(t, crypto.SHA256, []byte("signed with a compromised key"))

	pinned := gpgKey(t, "pinned", "ed25519", "sign", "")
	cosigner := gpgKey(t, "cosigner", "ed25519", "sign", "")
	revocation := revocationCertificate(t, pinned)

	// The attacker holds the (since revoked) pinned private key and any key the verifier trusts.
	sigPinned, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, pinned.privCreds())
	require.NoError(t, err)
	sigCosigner, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, cosigner.privCreds())
	require.NoError(t, err)
	combined := sigCosigner.Value + sigPinned.Value

	keyring := gpgKey(t, "keyring", "ed25519", "sign", "")
	for _, material := range []string{pinned.public, cosigner.public, revocation} {
		path := filepath.Join(t.TempDir(), "key.asc")
		require.NoError(t, os.WriteFile(path, []byte(material), 0o600))
		keyring.gpg(t, "--import", path)
	}
	t.Setenv("GNUPGHOME", keyring.home)

	tests := []struct {
		name  string
		cfg   *v1alpha1.Config
		creds *gpgcredentialsv1.GPGCredentials
	}{
		{
			name: "keyring",
			cfg:  &v1alpha1.Config{UseKeyring: true, KeyFingerprint: pinned.fpr},
		},
		{
			name:  "isolated home with the revocation in the public key material",
			cfg:   &v1alpha1.Config{KeyFingerprint: pinned.fpr},
			creds: &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: pinned.public + "\n" + revocation + "\n" + cosigner.public},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			// Precondition: the revoked key alone is rejected, as the PR describes.
			r.Error(h.Verify(t.Context(), gpgSignature(digest, sigPinned.Value), tc.cfg, tc.creds),
				"a signature by the revoked pinned key alone must fail")
			r.Error(h.Verify(t.Context(), gpgSignature(digest, combined), tc.cfg, tc.creds),
				"a signature by the revoked pinned key must fail even when another key co-signs")
		})
	}
}

// revocationCertificate returns the revocation certificate gpg generated for k, ready to import.
func revocationCertificate(t *testing.T, k *testKey) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(k.home, "openpgp-revocs.d", k.fpr+".rev"))
	require.NoError(t, err)
	// gpg prefixes the armor header with ":" so the certificate is not imported by accident.
	return strings.ReplaceAll(string(b), ":-----BEGIN PGP PUBLIC KEY BLOCK-----", "-----BEGIN PGP PUBLIC KEY BLOCK-----")
}

// Test_Integration_ReviewPR3691_F3_LongTMPDIR shows that signing fails when $TMPDIR is long:
// the isolated GnuPG home is created under os.TempDir(), and gpg-agent cannot bind its socket
// once the path exceeds the Unix socket path limit (104 bytes on macOS, 108 on Linux without
// /run/user). The test helper gpgKey already avoids t.TempDir() for exactly this reason.
func Test_Integration_ReviewPR3691_F3_LongTMPDIR(t *testing.T) {
	_, err := exec.LookPath("gpg")
	require.NoError(t, err, "GnuPG >= 2.2 must be on PATH")
	r := require.New(t)

	h := mustHandler(t)
	signer := gpgKey(t, "signer", "ed25519", "sign", "")
	digest := makeDigest(t, crypto.SHA256, []byte("long TMPDIR"))

	long, err := os.MkdirTemp("", "ocm-review-")
	r.NoError(err)
	t.Cleanup(func() { _ = os.RemoveAll(long) })
	long = filepath.Join(long, strings.Repeat("d", max(1, 100-len(long))))
	r.NoError(os.MkdirAll(long, 0o700))
	t.Setenv("TMPDIR", long)

	sig, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, signer.privCreds())
	r.NoError(err, "signing must not depend on the length of $TMPDIR (%d bytes)", len(long))
	r.NoError(h.Verify(t.Context(), gpgSignature(digest, sig.Value), &v1alpha1.Config{}, signer.pubCreds()))
}
