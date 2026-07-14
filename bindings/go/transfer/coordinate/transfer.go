// Package coordinate wires the technology-independent coordinate system into a transfer:
// it produces a coordinate from a source access (dispatched per source technology),
// downloads the content, and places it into a target technology via that target's
// Coordinator, keeping the original access as origin. The mapping is therefore N+M —
// one Coordinator per technology — never N*M.
package coordinate

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ReferenceHintLabel is the reserved, non-signed label under which the reference hint
// (neutral coordinate + typed origin) travels with a transferred resource.
const ReferenceHintLabel = "ocm.software/reference-hint"

// Registry dispatches ToCoordinate to the Coordinator registered for a source access type.
type Registry struct {
	byType map[string]repository.Coordinator
}

// NewRegistry returns an empty coordinator registry.
func NewRegistry() *Registry {
	return &Registry{byType: map[string]repository.Coordinator{}}
}

// Register associates a Coordinator with a source access type (and its aliases).
func (r *Registry) Register(c repository.Coordinator, types ...runtime.Type) {
	for _, t := range types {
		r.byType[t.String()] = c
	}
}

// ToCoordinate reduces a source access spec to a neutral coordinate by dispatching to the
// Coordinator registered for its type.
func (r *Registry) ToCoordinate(spec runtime.Typed) (*descriptor.Coordinate, error) {
	c, ok := r.byType[spec.GetType().String()]
	if !ok {
		return nil, fmt.Errorf("no coordinator registered for access type %q", spec.GetType())
	}
	return c.ToCoordinate(spec)
}

// Transfer moves a resource's content from a source repository to a target technology
// using the coordinate system:
//
//  1. produce the neutral coordinate from the source access (per source technology),
//     completed with the resource's version/extraIdentity;
//  2. download the content from the source;
//  3. place the coordinate into the target technology (targetCoordinator.FromCoordinate);
//  4. attach the reference hint (coordinate + typed origin access) as a non-signed label;
//  5. upload the content to the target, which returns the final (pinned) access.
//
// It works for any registered source technology and any target Coordinator without either
// side knowing the other's access spec.
func Transfer(
	ctx context.Context,
	sourceRepo repository.ResourceRepository,
	sourceRes *descriptor.Resource,
	sourceCredentials runtime.Typed,
	registry *Registry,
	targetCoordinator repository.Coordinator,
	targetRepo repository.ResourceRepository,
	targetCredentials runtime.Typed,
) (*descriptor.Resource, error) {
	if sourceRes == nil || sourceRes.Access == nil {
		return nil, fmt.Errorf("source resource with an access is required")
	}

	// 1. source access -> neutral coordinate, completed from the resource identity.
	coord, err := registry.ToCoordinate(sourceRes.Access)
	if err != nil {
		return nil, fmt.Errorf("producing coordinate from source access: %w", err)
	}
	if coord.Version == "" {
		coord.Version = sourceRes.Version
	}
	if len(sourceRes.ExtraIdentity) > 0 && coord.Qualifiers == nil {
		coord.Qualifiers = map[string]string{}
		maps.Copy(coord.Qualifiers, sourceRes.ExtraIdentity)
	}

	// keep the original, typed access as origin.
	origin, err := toRaw(sourceRes.Access)
	if err != nil {
		return nil, fmt.Errorf("capturing origin access: %w", err)
	}

	// 2. download from the source.
	content, err := sourceRepo.DownloadResource(ctx, sourceRes, sourceCredentials)
	if err != nil {
		return nil, fmt.Errorf("downloading source resource: %w", err)
	}

	// 3. neutral coordinate -> target access.
	targetAccess, err := targetCoordinator.FromCoordinate(coord)
	if err != nil {
		return nil, fmt.Errorf("placing coordinate into target: %w", err)
	}

	// 4. build the target resource and stamp the reference hint (coordinate + origin).
	target := sourceRes.DeepCopy()
	target.Access = targetAccess
	if err := setReferenceHint(target, descriptor.ReferenceHint{Coordinate: *coord, Origin: origin}); err != nil {
		return nil, fmt.Errorf("attaching reference hint: %w", err)
	}

	// 5. upload to the target; the target repo returns the final pinned access.
	uploaded, err := targetRepo.UploadResource(ctx, target, content, targetCredentials)
	if err != nil {
		return nil, fmt.Errorf("uploading to target: %w", err)
	}
	// the upload may replace the access; re-attach the hint so it survives.
	if err := setReferenceHint(uploaded, descriptor.ReferenceHint{Coordinate: *coord, Origin: origin}); err != nil {
		return nil, fmt.Errorf("re-attaching reference hint after upload: %w", err)
	}
	return uploaded, nil
}

// ReferenceHintOf reads the reference hint from a resource, if present.
func ReferenceHintOf(res *descriptor.Resource) (*descriptor.ReferenceHint, bool) {
	for i := range res.Labels {
		if res.Labels[i].Name == ReferenceHintLabel {
			var hint descriptor.ReferenceHint
			if json.Unmarshal(res.Labels[i].Value, &hint) == nil {
				return &hint, true
			}
		}
	}
	return nil, false
}

func setReferenceHint(res *descriptor.Resource, hint descriptor.ReferenceHint) error {
	value, err := json.Marshal(hint)
	if err != nil {
		return err
	}
	label := descriptor.Label{Name: ReferenceHintLabel, Value: value, Signing: false}
	for i := range res.Labels {
		if res.Labels[i].Name == ReferenceHintLabel {
			res.Labels[i] = label
			return nil
		}
	}
	res.Labels = append(res.Labels, label)
	return nil
}

func toRaw(access runtime.Typed) (*runtime.Raw, error) {
	data, err := json.Marshal(access)
	if err != nil {
		return nil, err
	}
	raw := &runtime.Raw{Data: data}
	raw.SetType(access.GetType())
	return raw, nil
}
