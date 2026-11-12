package remotestore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oras-project/oras-go/v3/errdef"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/stretchr/testify/require"
)

// The two tables below are the ones deleted with remote_store_test.go, re-pointed
// at the now-native v3 Untag, to check the contract survived the delegation.
func TestReviewPR4221_Untag_StatusMapping(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		wantErr    error
		wantAnyErr bool
	}{
		{name: "200 OK", statusCode: http.StatusOK},
		{name: "202 Accepted", statusCode: http.StatusAccepted},
		{name: "204 No Content", statusCode: http.StatusNoContent},
		{name: "404 Not Found", statusCode: http.StatusNotFound, wantErr: errdef.ErrNotFound},
		{name: "405 Method Not Allowed", statusCode: http.StatusMethodNotAllowed, wantAnyErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			t.Cleanup(srv.Close)

			repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
			r.NoError(err)
			repo.Registry.PlainHTTP = true
			repo.Registry.Client = &http.Client{}
			store := &RemoteStore{Repository: repo}

			err = store.Untag(t.Context(), "latest")
			switch {
			case tc.wantErr != nil:
				r.ErrorIs(err, tc.wantErr)
			case tc.wantAnyErr:
				r.Error(err)
				t.Logf("405 surfaces as: %v", err)
			default:
				r.NoError(err)
			}
		})
	}
}

func TestReviewPR4221_Untag_ReferenceValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name      string
		reference string
		wantErr   bool
	}{
		{"latest", "latest", false},
		{"stable", "stable", false},
		{"edge", "edge", false},
		{"my-component-alias", "my-component-alias", false},
		{"digest sha256", "sha256:44136fa355ba77b9ad7b9e1dabe2dfe34ef0e91e2b5bff3e5e24b0bf09f28cbb", true},
		{"digest sha512", "sha512:abc123def456", true},
		{"empty string", "", true},
		{"colon separator", "not:a:tag", true},
		{"leading dot", ".badtag", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
			r.NoError(err)
			repo.Registry.PlainHTTP = true
			repo.Registry.Client = &http.Client{}
			store := &RemoteStore{Repository: repo}

			err = store.Untag(t.Context(), tc.reference)
			if tc.wantErr {
				r.Error(err, "reference %q must be rejected", tc.reference)
				return
			}
			r.NoError(err)
		})
	}
}
