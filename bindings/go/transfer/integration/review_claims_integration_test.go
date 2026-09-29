package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

func reviewPut(t *testing.T, target, password string, data []byte) {
	t.Helper()
	r := require.New(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, target, bytes.NewReader(data))
	r.NoError(err)
	req.SetBasicAuth("admin", password)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.True(resp.StatusCode >= 200 && resp.StatusCode < 300, "PUT %s: %d %s", target, resp.StatusCode, body)
}

func TestReviewNexusClaims(t *testing.T) {
	base, password := startNexus(t)
	t.Run("POMCoordinates", func(t *testing.T) {
		r := require.New(t)
		status, body := nexusRequest(t, http.MethodPost, base+"/service/rest/v1/repositories/maven/hosted", password, []byte(`{"name":"review-maven","online":true,"storage":{"blobStoreName":"default","strictContentTypeValidation":false,"writePolicy":"allow"},"maven":{"versionPolicy":"RELEASE","layoutPolicy":"STRICT","contentDisposition":"INLINE"}}`))
		r.Equal(http.StatusCreated, status, string(body))
		actual := base + "/repository/review-maven/org/example/actual/2.0/actual-2.0.pom"
		original := []byte(`<project><modelVersion>4.0.0</modelVersion><groupId>org.example</groupId><artifactId>actual</artifactId><version>2.0</version><description>original</description></project>`)
		replacement := bytes.ReplaceAll(original, []byte("original"), []byte("replacement"))
		reviewPut(t, actual, password, original)
		repo, source := addWgetComponent(t, "ocm.software/review-pom", "1.0.0", wgetFile{name: "input.pom", resource: "pom", version: "1.0.0", mediaType: "application/xml", data: replacement})
		targetPath, target := newTargetCTF(t)
		up := &transferv1alpha1.NexusUploaderConfig{Type: runtime.NewVersionedType(transferv1alpha1.NexusUploaderConfigType, transferv1alpha1.Version), MatchSpec: transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType(wgetaccess.WgetConsumerType, wgetaccessv1.Version)}, URL: base, Repository: "review-maven", Path: "com/example/demo/1.0/demo-1.0.pom"}
		transferOnce(t, repo, source, target, up, wgetrepository.NewResourceRepository(nil), nexusCredentials(t, base, password, "review-maven"), "ocm.software/review-pom", "1.0.0")
		desc, err := createCTFRepository(t, targetPath).GetComponentVersion(t.Context(), "ocm.software/review-pom", "1.0.0")
		r.NoError(err)
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(desc.Component.Resources[0].Access, &access))
		publishedStatus, _ := nexusRequest(t, http.MethodGet, access.URL, password, nil)
		actualBody := nexusGet(t, actual, password)
		t.Logf("transfer succeeded; published URL=%s status=%d; actual POM changed=%v", access.URL, publishedStatus, !bytes.Equal(original, actualBody))
		if publishedStatus != http.StatusOK {
			t.Errorf("published URL must resolve: got %d", publishedStatus)
		}
		if !bytes.Equal(original, actualBody) {
			t.Errorf("unchecked POM destination was modified")
		}
	})
	t.Run("Pagination", func(t *testing.T) {
		r := require.New(t)
		data := []byte("same payload")
		for i := range 65 {
			reviewPut(t, fmt.Sprintf("%s/repository/raw-hosted/review/blob%03d", base, i), password, data)
		}
		reviewPut(t, base+"/repository/raw-hosted/review/"+url.PathEscape("blob*"), password, data)
		query := url.Values{"repository": {"raw-hosted"}, "name": {"/review/blob*"}}
		type page struct {
			Items []struct {
				Path string `json:"path"`
			} `json:"items"`
			Token string `json:"continuationToken"`
		}
		var first, second page
		r.Eventually(func() bool {
			body := nexusGet(t, base+"/service/rest/v1/search/assets?"+query.Encode(), password)
			r.NoError(json.Unmarshal(body, &first))
			if first.Token == "" {
				return false
			}
			next := url.Values{"repository": {"raw-hosted"}, "name": {"/review/blob*"}, "continuationToken": {first.Token}}
			body = nexusGet(t, base+"/service/rest/v1/search/assets?"+next.Encode(), password)
			r.NoError(json.Unmarshal(body, &second))
			for _, item := range second.Items {
				if item.Path == "/review/blob*" {
					return true
				}
			}
			return false
		}, 15*time.Second, 500*time.Millisecond, "exact target should be indexed on page two")
		for _, item := range first.Items {
			r.NotEqual("/review/blob*", item.Path)
		}
		t.Logf("first page items=%d continuationToken=%q; second page items=%d includes exact target /review/blob*", len(first.Items), first.Token, len(second.Items))
		r.Equal(data, nexusGet(t, base+"/repository/raw-hosted/review/"+url.PathEscape("blob*"), password))
		repo, source := addWgetComponent(t, "ocm.software/review-pagination", "1.0.0", wgetFile{name: "input.txt", resource: "blob", version: "1.0.0", mediaType: "text/plain", data: data})
		_, target := newTargetCTF(t)
		up := &transferv1alpha1.NexusUploaderConfig{Type: runtime.NewVersionedType(transferv1alpha1.NexusUploaderConfigType, transferv1alpha1.Version), MatchSpec: transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType(wgetaccess.WgetConsumerType, wgetaccessv1.Version)}, URL: base, Repository: "raw-hosted", Path: "review/blob*"}
		transferOnce(t, repo, source, target, up, wgetrepository.NewResourceRepository(nil), nexusCredentials(t, base, password, "raw-hosted"), "ocm.software/review-pagination", "1.0.0")
	})
}
