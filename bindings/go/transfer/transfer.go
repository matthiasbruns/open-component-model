package transfer

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/coordinate"
	"ocm.software/open-component-model/bindings/go/transfer/internal"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// BuildOption configures optional graph-building behavior, e.g. coordinate-based S3 upload.
type BuildOption = internal.BuildOption

// WithS3Upload enables coordinate-based upload to S3 (used with UploadType s3): source accesses
// are reduced to a neutral coordinate via the registry and placed into S3 via the target Coordinator.
func WithS3Upload(coordinates *coordinate.Registry, target repository.Coordinator) BuildOption {
	return internal.WithS3Upload(coordinates, target)
}

// WithCoordinates enables coordinate-based localization of external accesses (e.g. downloading an
// S3 source into a localBlob), stamping the coordinate and origin.
func WithCoordinates(coordinates *coordinate.Registry) BuildOption {
	return internal.WithCoordinates(coordinates)
}

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] that
// describes how to transfer component versions between repositories.
//
// cfg carries the declarative transfer settings. A nil cfg and empty enum
// fields resolve to the defaults: no recursion,
// [transferv1alpha1.CopyModeLocalBlobResources], and
// [transferv1alpha1.UploadAsDefault].
//
// Each [Mapping] pairs source components with a target repository and a
// resolver, enabling N:M routing where different sources feed different
// targets.
func BuildGraphDefinition(
	ctx context.Context,
	cfg *transferv1alpha1.Config,
	mappings ...Mapping,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	return BuildGraphDefinitionWithOptions(ctx, cfg, nil, mappings...)
}

// BuildGraphDefinitionWithOptions is [BuildGraphDefinition] with additional build options, e.g.
// [WithS3Upload] for coordinate-based S3 upload.
func BuildGraphDefinitionWithOptions(
	ctx context.Context,
	cfg *transferv1alpha1.Config,
	opts []BuildOption,
	mappings ...Mapping,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid transfer config: %w", err)
	}

	resolved := transferv1alpha1.Config{}
	if cfg != nil {
		resolved = *cfg
	}
	if resolved.CopyMode == "" {
		resolved.CopyMode = transferv1alpha1.CopyModeLocalBlobResources
	}
	if resolved.UploadType == "" {
		resolved.UploadType = transferv1alpha1.UploadAsDefault
	}

	roots, err := collectTransferRoots(ctx, mappings)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "building transfer graph definition",
		"roots", len(roots),
		"recursive", resolved.Recursive,
		"copyMode", resolved.CopyMode,
		"uploadType", resolved.UploadType)

	return internal.BuildGraphDefinition(ctx, roots, resolved, opts...)
}

func collectTransferRoots(ctx context.Context, mappings []Mapping) (map[string]internal.TransferRoot, error) {
	if len(mappings) == 0 {
		return nil, fmt.Errorf("no transfer mappings specified")
	}

	type rootData struct {
		targets  []runtime.Typed
		resolver resolvers.ComponentVersionRepositoryResolver
	}

	byKey := make(map[string]*rootData)

	for i, m := range mappings {
		if m.Target == nil {
			return nil, fmt.Errorf("mapping %d has no target", i)
		}
		if m.Resolver == nil {
			return nil, fmt.Errorf("mapping %d has no resolver", i)
		}

		ids, err := resolveMapping(ctx, &m)
		if err != nil {
			return nil, fmt.Errorf("mapping %d: %w", i, err)
		}

		slog.DebugContext(ctx, "resolved transfer mapping",
			"mapping", i,
			"components", len(ids),
			"target", fmt.Sprintf("%T", m.Target))

		for _, id := range ids {
			key := id.String()
			rd, exists := byKey[key]
			if !exists {
				rd = &rootData{resolver: m.Resolver}
				byKey[key] = rd
			} else if rd.resolver != m.Resolver {
				return nil, fmt.Errorf("conflicting resolvers for component %s: each component must use the same resolver across all mappings", key)
			}
			rd.targets = internal.AppendUniqueRepositories(rd.targets, []runtime.Typed{m.Target})
		}
	}

	roots := make(map[string]internal.TransferRoot, len(byKey))
	for key, rd := range byKey {
		roots[key] = internal.TransferRoot{
			RootComponentKey: key,
			Targets:          rd.targets,
			SourceResolver:   rd.resolver,
		}
	}
	return roots, nil
}

func resolveMapping(ctx context.Context, m *Mapping) ([]ComponentID, error) {
	if m.ComponentLister != nil && len(m.Components) > 0 {
		return nil, fmt.Errorf("cannot combine Components with ComponentLister in the same mapping")
	}

	if m.ComponentLister != nil {
		var ids []ComponentID
		if err := m.ComponentLister.ListComponentVersions(ctx, func(batch []ComponentID) error {
			ids = append(ids, batch...)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("listing components failed: %w", err)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("component lister returned no components")
		}
		return ids, nil
	}

	if len(m.Components) == 0 {
		return nil, fmt.Errorf("no components specified in mapping")
	}
	return m.Components, nil
}
