// Package customca contains a CLI integration matrix test that verifies wget's
// TLS behaviour for a custom server CA configured through OCM's standardized
// mechanism — the SSL_CERT_FILE environment variable read by Go's crypto/tls
// stack — independently of the mTLS client certificate carried in wget credentials.
//
// It lives in its own package (and therefore its own test binary) on purpose: Go
// loads and caches the system certificate pool once per process (sync.Once), so
// SSL_CERT_FILE must be set before the first TLS handshake and no other HTTPS client
// may prime the pool first. That isolation cannot be guaranteed inside the shared
// `package integration` test binary, so this test cannot move there.
package customca

import (
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_WgetCustomCA verifies, over a matrix of trusted/untrusted server
// CAs, that a wget resource downloaded over HTTPS is accepted only when the server
// certificate chains to a CA that OCM trusts via SSL_CERT_FILE. No client
// certificate (mTLS) is involved — this exercises server-trust only.
//
// SSL_CERT_FILE is set once, to the "trusted" CA. Each case serves its artifact with
// a certificate signed by a different CA, so the process-wide cert pool loads the
// trusted CA at the first handshake and both outcomes are deterministic regardless of
// case order.
//
// This test must not run in parallel: it uses t.Setenv and relies on being the sole
// HTTPS client in this test binary.
func Test_Integration_WgetCustomCA(t *testing.T) {
	trustedCA, trustedKey, trustedPEM := internal.NewCA(t)
	untrustedCA, untrustedKey, _ := internal.NewCA(t)

	// Trust only the "trusted" CA through OCM's documented custom-CA mechanism.
	caFile := filepath.Join(t.TempDir(), "trusted-ca.pem")
	require.NoError(t, os.WriteFile(caFile, trustedPEM, 0o600))
	t.Setenv("SSL_CERT_FILE", caFile)

	cases := []struct {
		name      string
		serverCA  *x509.Certificate
		serverKey *ecdsa.PrivateKey
		trusted   bool // whether serverCA is the one advertised via SSL_CERT_FILE
	}{
		{"server cert signed by the SSL_CERT_FILE CA is accepted", trustedCA, trustedKey, true},
		// SSL_CERT_FILE still points at the trusted CA here; only the server's signing
		// CA differs, so on Linux this fails specifically because the presented cert is
		// not signed by the configured CA (not merely because it is a private CA).
		{"server cert signed by a different CA is rejected", untrustedCA, untrustedKey, false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// SSL_CERT_FILE is only consulted by Go's crypto/x509 on Linux and other
			// unix targets that use the file-based root loader. macOS and Windows use
			// the platform verifier, which ignores it, so the positive (trusted) case
			// cannot be exercised there. The negative case holds on every platform.
			if tc.trusted && runtime.GOOS != "linux" {
				t.Skipf("SSL_CERT_FILE is not honored by Go's TLS stack on %s (platform verifier); skipping trusted-CA case", runtime.GOOS)
			}

			r := require.New(t)

			content := []byte(fmt.Sprintf("artifact for case %d", i))
			srv := internal.NewTLSServerWithCert(t, internal.IssueServerCert(t, tc.serverCA, tc.serverKey), content)
			r.True(strings.HasPrefix(srv.URL, "https://"), "test server must serve HTTPS")

			name, version := fmt.Sprintf("ocm.software/wget-customca-%d", i), "v1.0.0"
			tmp := t.TempDir()
			constructorPath := filepath.Join(tmp, "constructor.yaml")
			r.NoError(os.WriteFile(constructorPath, []byte(internal.WgetInputConstructor(name, version, srv.URL)), 0o600))
			archive := filepath.Join(tmp, "transport-archive")

			// The wget input downloads the artifact over HTTPS at add time.
			addCMD := cmd.New()
			addCMD.SetArgs([]string{
				"add", "component-version",
				"--repository", archive,
				"--constructor", constructorPath,
			})
			err := addCMD.ExecuteContext(t.Context())

			if !tc.trusted {
				r.Error(err, "wget input over HTTPS must fail when the server's CA is not trusted")
				r.Contains(strings.ToLower(err.Error()), "certificate",
					"failure should be a TLS certificate verification error, got: %v", err)
				return
			}

			r.NoError(err, "wget input over HTTPS must succeed when SSL_CERT_FILE trusts the server's CA")

			// Round-trip the stored blob to confirm the fetched bytes are intact.
			output := filepath.Join(tmp, "downloaded-blob")
			downloadCMD := cmd.New()
			downloadCMD.SetArgs([]string{
				"download", "resource",
				fmt.Sprintf("%s//%s:%s", archive, name, version),
				"--identity", "name=remote-blob,version=v1.0.0",
				"--output", output,
			})
			r.NoError(downloadCMD.ExecuteContext(t.Context()), "download of the wget-sourced resource must succeed")

			got, err := os.ReadFile(output)
			r.NoError(err)
			r.Equal(content, got, "downloaded resource must match the content served over HTTPS")
		})
	}
}
