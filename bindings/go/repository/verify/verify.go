package verify

import (
	"context"
	"errors"
	"fmt"
	"io"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// VerifyDownload verifies content with digest `res` contains, as it is read.
//
// A resource that has no digest passes through unverified with a warning, but a
// digest that is present and unusable errors. Content that cannot be checked
// against the digest must not pass as verified.
func VerifyDownload(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	verifier, err := NewGenericResourceVerifierProvider(VerifyIfPresent).GetResourceVerifier(ctx, res)
	if err != nil {
		return nil, handlerError(content, err)
	}

	verifying, err := verifier.Verify(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("cannot verify resource %q against digest: %w", res.Name, err)
	}

	return verifying, nil
}

// handlerError closes the reader and wraps the error into something that makes sense.
func handlerError(content blob.ReadOnlyBlob, err error) error {
	closer, ok := content.(io.Closer)
	if !ok {
		return err
	}
	if closeErr := closer.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("failed to release unverifiable content: %w", closeErr))
	}

	return err
}
