package v1

import "ocm.software/open-component-model/bindings/go/runtime"

const MavenCredentialsType = "MavenCredentials"

var MavenCredentialsVersionedType = runtime.NewVersionedType(MavenCredentialsType, Version)

// MustRegisterCredentialType registers MavenCredentials/v1 (and its unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&MavenCredentials{},
		MavenCredentialsVersionedType,
		runtime.NewUnversionedType(MavenCredentialsType),
	)
}

// MavenCredentials represents typed credentials for Maven authentication.
//
// TODO: replace the sample fields below with the real credential parameters.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type MavenCredentials struct {
	// +ocm:jsonschema-gen:enum=MavenCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=MavenCredentials
	Type runtime.Type `json:"type"`
	// Username is the username for HTTP Basic Authentication. Used together with Password.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP Basic Authentication. Used together with Username.
	Password string `json:"password,omitempty"`
	// IdentityToken is a bearer token sent as "Authorization: Bearer <token>".
	IdentityToken string `json:"identityToken,omitempty"`
}
