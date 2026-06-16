// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gpg

import (
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	gpgcredspec "ocm.software/open-component-model/bindings/go/gpg/spec/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
)

func Register(
	signingHandlerRegistry *signinghandler.SigningRegistry,
	credTypeRegistry *credentialtype.Registry,
	_ *filesystemv1alpha1.Config,
) error {
	hdlr, err := handler.New(nil)
	if err != nil {
		return err
	}

	credTypeRegistry.Register(gpgcredspec.Scheme)

	return signingHandlerRegistry.RegisterInternalComponentSignatureHandler(hdlr)
}
