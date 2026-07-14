// Package coordinate implements the technology-independent coordinate mapping for the
// Helm access type.
package coordinate

import (
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Config describes where FromCoordinate places content in a Helm environment.
type Config struct {
	// HelmRepository is the target chart repository URL.
	HelmRepository string
}

// Coordinator maps between Helm accesses and neutral coordinates.
type Coordinator struct {
	target Config
}

var _ repository.Coordinator = (*Coordinator)(nil)

// New returns a Coordinator that places charts into the given helm repository.
func New(target Config) *Coordinator {
	return &Coordinator{target: target}
}

// ToCoordinate reduces a Helm access to the neutral coordinate: the chart name becomes
// Path and the chart version becomes Version. The repository URL is environment-specific
// and intentionally dropped.
func (c *Coordinator) ToCoordinate(spec runtime.Typed) (*descriptor.Coordinate, error) {
	var h v1.Helm
	if err := helmaccess.Scheme.Convert(spec, &h); err != nil {
		return nil, fmt.Errorf("error converting access spec to helm: %w", err)
	}
	name, version := h.HelmChart, h.Version
	if i := strings.LastIndex(h.HelmChart, ":"); i >= 0 {
		name = h.HelmChart[:i]
		if version == "" {
			version = h.HelmChart[i+1:]
		}
	}
	if name == "" {
		return nil, fmt.Errorf("helm access has no chart name to derive a coordinate")
	}
	return &descriptor.Coordinate{
		Path:      name,
		Version:   version,
		MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
	}, nil
}

// FromCoordinate expands any coordinate into a Helm access in the configured repository:
// helmChart = <path>:<version>.
func (c *Coordinator) FromCoordinate(coord *descriptor.Coordinate) (runtime.Typed, error) {
	if coord == nil {
		return nil, fmt.Errorf("coordinate is required")
	}
	if c.target.HelmRepository == "" {
		return nil, fmt.Errorf("target helm repository is required to place a coordinate in helm")
	}
	if coord.Path == "" {
		return nil, fmt.Errorf("coordinate has no path to place as a helm chart name")
	}
	chart := coord.Path
	if coord.Version != "" {
		chart += ":" + coord.Version
	}
	return &v1.Helm{
		Type:           runtime.NewVersionedType(v1.Type, v1.Version),
		HelmRepository: c.target.HelmRepository,
		HelmChart:      chart,
	}, nil
}
