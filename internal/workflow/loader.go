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
}

func NewRegistry(dir string) *Registry {
	return &Registry{
		definitions: make(map[string]*Definition),
		dir:         dir,
	}
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

	r.mu.Lock()
	r.definitions = loaded
	r.mu.Unlock()
	return nil
}

func (r *Registry) Get(name string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.definitions[name]
	return def, ok
}

func (r *Registry) List() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.definitions))
	for _, d := range r.definitions {
		out = append(out, d)
	}
	return out
}
