package fips140

import (
	"bytes"
	"crypto"
	"crypto/fips140"
	"errors"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Review evidence (PR 3858): in strict mode a v6 RSA-1024 key is rejected, but by go-crypto's self-signature check
// during parsing, not with ErrAlgorithmInFIPSMode, so the error does not say what to change.
func TestReview_StrictRSA1024Error(t *testing.T) {
	requireStrict(t)
	var priv, pub bytes.Buffer
	fips140.WithoutEnforcement(func() {
		e, err := openpgp.NewEntity("rsa1024", "", "rsa1024@example.com", &packet.Config{
			V6Keys: true, Algorithm: packet.PubKeyAlgoRSA, RSABits: 1024, DefaultHash: crypto.SHA256,
		})
		require.NoError(t, err)
		w, _ := armor.Encode(&priv, openpgp.PrivateKeyType, nil)
		require.NoError(t, e.SerializePrivate(w, nil))
		require.NoError(t, w.Close())
		w, _ = armor.Encode(&pub, openpgp.PublicKeyType, nil)
		require.NoError(t, e.Serialize(w))
		require.NoError(t, w.Close())
	})
	_, err := mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: priv.String()})
	require.Error(t, err)
	t.Logf("sign error: %v", err)
	require.True(t, errors.Is(err, handler.ErrAlgorithmInFIPSMode), "sign: want ErrAlgorithmInFIPSMode, got %v", err)
}
