package componentversion

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

func TestReviewWithVerifiedTimePreservesCredentials(t *testing.T) {
	r := require.New(t)
	verifiedTime := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	root := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    verifiedTime.Add(-time.Hour), NotAfter: verifiedTime.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	r.NoError(err)

	for _, material := range []struct {
		name  string
		block *pem.Block
	}{
		{"public_key", &pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}},
		{"root_certificate", &pem.Block{Type: "CERTIFICATE", Bytes: rootDER}},
	} {
		t.Run(material.name, func(t *testing.T) {
			r := require.New(t)
			direct := &credconfigv1.DirectCredentials{
				Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
				Properties: map[string]string{"publicKeyPEM": string(pem.EncodeToMemory(material.block))},
			}
			data, err := json.Marshal(direct)
			r.NoError(err)
			for _, representation := range []struct {
				name  string
				creds runtime.Typed
			}{
				{"direct_control", direct},
				// Credential plugins return the same Credentials/v1 envelope as runtime.Raw.
				{"raw_plugin", &runtime.Raw{Type: direct.Type, Data: data}},
			} {
				t.Run(representation.name, func(t *testing.T) {
					r := require.New(t)
					before, err := rsacredentialsv1.ConvertToRSACredentials(representation.creds)
					r.NoError(err)
					r.Equal(direct.Properties["publicKeyPEM"], before.PublicKeyPEM)

					wrapped := withVerifiedTime(representation.creds, verifiedTime)
					gotTime, present, err := rsacredentialsv1.VerifiedTimeFromCredentials(wrapped)
					r.NoError(err)
					r.True(present)
					r.True(verifiedTime.Equal(gotTime))
					_, mutated := direct.Properties[tsa.VerifiedTimeKey]
					r.False(mutated, "wrapping must not mutate the original credentials")

					after, err := rsacredentialsv1.ConvertToRSACredentials(wrapped)
					r.NoError(err)
					r.Equal(before, after, "adding verified time must preserve RSA key/trust material")
				})
			}
		})
	}
}
