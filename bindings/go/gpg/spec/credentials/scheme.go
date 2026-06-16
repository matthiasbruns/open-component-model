// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package credentials

import (
	"ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme contains the GPG credential payload types provided by this package
// (currently GPGCredentials/v1alpha1). Pass it to CredentialTypeRegistry.Register
// so the credential graph can deserialize typed GPG credentials it finds in
// configuration.
var Scheme = runtime.NewScheme()

func init() {
	v1alpha1.MustRegisterCredentialType(Scheme)
}
