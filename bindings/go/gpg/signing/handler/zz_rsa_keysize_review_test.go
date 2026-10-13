package handler

import (
	"bytes"
	"crypto"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Review evidence (PR 3858): with fips140=on, a v6 RSA-1024 key signs without any FIPS boundary debug log,
// although ADR 0030 requires steps outside the boundary to be logged.
func TestReview_RSA1024NoBoundaryLog(t *testing.T) {
	r := require.New(t)
	logs := captureDebugLogs(t)
	e, err := openpgp.NewEntity("rsa1024", "", "rsa1024@example.com", &packet.Config{
		V6Keys: true, Algorithm: packet.PubKeyAlgoRSA, RSABits: 1024, DefaultHash: crypto.SHA256,
	})
	r.NoError(err)
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	r.NoError(err)
	r.NoError(e.SerializePrivate(w, nil))
	r.NoError(w.Close())

	logs.take()
	_, err = mustHandler(t).Sign(t.Context(), makeDigest(t, crypto.SHA256, testDigestContent), &v1alpha1.Config{},
		&gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: buf.String()})
	r.NoError(err)
	var boundary []string
	for _, msg := range logs.take() {
		if strings.Contains(msg, "FIPS 140-3 boundary") {
			boundary = append(boundary, msg)
		}
	}
	r.NotEmpty(boundary, "expected a FIPS boundary debug log for an RSA-1024 key")
}
