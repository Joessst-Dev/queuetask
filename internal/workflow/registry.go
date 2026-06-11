package workflow

import (
	"fmt"
	"path/filepath"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]*Definition
	dir         string
	hooks       []func([]*Definition)
}

func NewRegistry(dir string) *Registry {
	return &Registry{
		definitions: make(map[string]*Definition),
		dir:         dir,
	}
}

// AddReloadHook registers a function that is called after every successful Load.
// Hooks are called outside the registry lock with the freshly loaded definitions.
func (r *Registry) AddReloadHook(fn func([]*Definition)) {
	r.mu.Lock()
	r.hooks = append(r.hooks, fn)
	r.mu.Unlock()
}

func (r *Registry) Load() error {
	pattern := filepath.Join(r.dir, "*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("globbing %s: %w", pattern, err)
	}

	loaded := make(map[string]*Definition, len(files))
	for _, f := range files {
		def, err := ParseFile(f)
		if err != nil {
			return err
		}
		if _, dup := loaded[def.Name]; dup {
			return fmt.Errorf("duplicate workflow name %q (found again in %s)", def.Name, f)
		}
		loaded[def.Name] = def
	}

	defs := make([]*Definition, 0, len(loaded))
	for _, d := range loaded {
		defs = append(defs, d)
	}

	r.mu.Lock()
	r.definitions = loaded
	hooks := r.hooks
	r.mu.Unlock()

	for _, hook := range hooks {
		hook(defs)
	}
	return nil
}

func (r *Registry) Get(name string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.definitions[name]
	return def, ok
}

func (r *Registry) Dir() string { return r.dir }

func (r *Registry) List() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.definitions))
	for _, d := range r.definitions {
		out = append(out, d)
	}
	return out
}
