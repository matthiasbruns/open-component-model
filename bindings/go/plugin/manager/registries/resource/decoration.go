package resource

// SetRepositoryDecorator configures an adapter applied on subsequent lookups of
// both built-in and external repositories. It does not change stored plugins or
// previously returned repositories. Configure it during application setup; nil
// restores undecorated lookup. The decorator runs under the registry lock and
// must not call back into the registry.
func (r *ResourceRegistry) SetRepositoryDecorator(decorator func(Repository) Repository) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decorator = decorator
}

func (r *ResourceRegistry) decorate(base Repository) Repository {
	if r.decorator == nil {
		return base
	}
	return r.decorator(base)
}
