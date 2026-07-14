package v1alpha1

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

// ConfigVersionedType is the versioned type of the S3 uploader configuration.
var ConfigVersionedType = runtime.NewVersionedType(ConfigType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		ConfigVersionedType,
		runtime.NewUnversionedType(ConfigType),
	)
}

// LookupConfig extracts the S3 uploader configuration from a central generic config. It returns
// nil when no uploader.s3 entry is present. When multiple entries exist, the last one wins.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			ConfigVersionedType,
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	if len(filtered.Configurations) == 0 {
		return nil, nil
	}
	var config Config
	if err := Scheme.Convert(filtered.Configurations[len(filtered.Configurations)-1], &config); err != nil {
		return nil, fmt.Errorf("failed to decode s3 uploader config: %w", err)
	}
	return &config, nil
}
