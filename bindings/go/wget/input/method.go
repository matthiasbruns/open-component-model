package input

import (
	"context"
	"fmt"
	nethttp "net/http"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/access"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	"ocm.software/open-component-model/bindings/go/wget/repository"
	input "ocm.software/open-component-model/bindings/go/wget/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the ResourceInputMethod interface for wget-based inputs.
// It downloads a resource from an HTTP/S URL declared in the component constructor
// and returns it as a local blob to be stored in the component version.
//
// The download transport, credential handling and size limiting are shared with the
// wget access type via the internal download package: this method converts the wget
// input specification into a download request, while the access type converts the
// wget access specification into the same request.
type InputMethod struct {
	// HTTPConfig configures the HTTP client (timeouts, retries, TLS, routing) used for
	// downloads. When nil, a default client is used.
	HTTPConfig *httpv1alpha1.Config
	// MaxDownloadSize limits the number of bytes read from a response body. When zero,
	// DefaultMaxDownloadSize is used. A negative value disables the limit.
	MaxDownloadSize int64
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for a
// wget input from its URL, using the same wget consumer type as the access type so that
// credentials configured for a host resolve for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	return access.CredentialConsumerIdentity(wget.URL)
}

// ProcessResource downloads the resource described by the wget input specification and
// returns it as local blob data. The download itself is delegated to the shared wget
// download logic, so behavior matches the wget access type exactly.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required in wget input spec")
	}

	var client *nethttp.Client
	if i.HTTPConfig != nil {
		client = httpclient.New(httpclient.WithConfig(i.HTTPConfig))
	}

	maxDownloadSize := i.MaxDownloadSize
	switch {
	case maxDownloadSize == 0:
		maxDownloadSize = repository.DefaultMaxDownloadSize
	case maxDownloadSize < 0:
		maxDownloadSize = 0
	}

	data, err := download.Download(ctx, client, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}, maxDownloadSize, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading wget input from %q: %w", wget.URL, err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: data,
	}, nil
}
