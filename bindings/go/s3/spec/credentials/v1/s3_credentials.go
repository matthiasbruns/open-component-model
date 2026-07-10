package v1

import "ocm.software/open-component-model/bindings/go/runtime"

const S3CredentialsType = "S3Credentials"

var S3CredentialsVersionedType = runtime.NewVersionedType(S3CredentialsType, Version)

// MustRegisterCredentialType registers S3Credentials/v1 (and its unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&S3Credentials{},
		S3CredentialsVersionedType,
		runtime.NewUnversionedType(S3CredentialsType),
	)
}

// S3Credentials represents typed credentials for S3 authentication.
//
// TODO: replace the sample fields below with the real credential parameters.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type S3Credentials struct {
	// +ocm:jsonschema-gen:enum=S3Credentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=S3Credentials
	Type runtime.Type `json:"type"`
	// Username is the username for HTTP Basic Authentication. Used together with Password.
	Username string `json:"username,omitempty"`
	// Password is the password for HTTP Basic Authentication. Used together with Username.
	Password string `json:"password,omitempty"`
	// IdentityToken is a bearer token sent as "Authorization: Bearer <token>".
	IdentityToken string `json:"identityToken,omitempty"`
}
