package tsa

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"net/http/httptest"
	"testing"

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/require"
)

func TestReviewVerify_EmbeddedIntermediate(t *testing.T) {
	r := require.New(t)

	rootKey, root := mustTSAKeyAndCert(t)
	root.Subject.CommonName = "Review Root"
	root.RawSubject = nil
	root.KeyUsage = x509.KeyUsageCertSign
	root.ExtKeyUsage = nil
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	r.NoError(err)
	root, err = x509.ParseCertificate(rootDER)
	r.NoError(err)

	intermediateKey, intermediate := mustTSAKeyAndCert(t)
	intermediate.Subject.CommonName = "Review Intermediate"
	intermediate.RawSubject = nil
	intermediate.KeyUsage = x509.KeyUsageCertSign
	intermediate.ExtKeyUsage = nil
	intermediateDER, err := x509.CreateCertificate(rand.Reader, intermediate, root, &intermediateKey.PublicKey, rootKey)
	r.NoError(err)
	intermediate, err = x509.ParseCertificate(intermediateDER)
	r.NoError(err)

	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	tsaCert.IsCA = false
	// Preserve the helper's critical, exclusive timestamping EKU when reissuing.
	for _, ext := range tsaCert.Extensions {
		if ext.Id.Equal(oidExtKeyUsage) {
			tsaCert.ExtraExtensions = append(tsaCert.ExtraExtensions, ext)
		}
	}
	tsaDER, err := x509.CreateCertificate(rand.Reader, tsaCert, intermediate, &tsaKey.PublicKey, intermediateKey)
	r.NoError(err)
	tsaCert, err = x509.ParseCertificate(tsaDER)
	r.NoError(err)
	r.NoError(verifyTimestampingEKU(tsaCert))

	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)
	digest := sha256.Sum256([]byte("embedded intermediate regression"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)
	p7, err := pkcs7.Parse(token.Raw)
	r.NoError(err)
	sd, err := pkcs7.NewSignedData(p7.Content)
	r.NoError(err)
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	r.NoError(sd.AddSigner(tsaCert, tsaKey, pkcs7.SignerInfoConfig{}))
	sd.AddCertificate(intermediate)
	raw, err := sd.Finish()
	r.NoError(err)
	p7, err = pkcs7.Parse(raw)
	r.NoError(err)
	r.Len(p7.Certificates, 2)

	roots := x509.NewCertPool()
	roots.AddCert(root)
	t.Run("explicit intermediates control", func(t *testing.T) {
		r := require.New(t)
		intermediates := x509.NewCertPool()
		intermediates.AddCert(intermediate)
		r.NoError(p7.VerifyWithOpts(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			CurrentTime:   token.Time,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		}))
	})
	t.Run("embedded intermediate with root-only trust", func(t *testing.T) {
		r := require.New(t)
		genTime, trusted, err := Verify(raw, crypto.SHA256, digest[:], roots)
		r.NoError(err, "Verify must build the signer chain using the token's embedded intermediate")
		r.True(trusted)
		r.Equal(token.Time, genTime)
	})
}
