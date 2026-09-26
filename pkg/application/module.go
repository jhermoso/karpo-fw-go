package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Module is a bounded context plugged into a host process (equivalent to the C# IBoundedContext).
// Modules declare their dependencies by name, mirroring the Karpo hierarchy
// (Fw <- ErpKernel <- ErpDetail <- Sectorial...). Optional lifecycle hooks: Starter, Stopper.
type Module interface {
	Name() string
	Dependencies() []string
}

// Starter is implemented by modules needing initialization (migrations, relays, caches...).
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is implemented by modules holding resources.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Host validates module dependencies and runs their lifecycle in dependency order.
type Host struct {
	ordered []Module
	started int
}

// NewHost orders modules so every module comes after its dependencies. It fails on duplicate
// names, unknown dependencies and cycles.
func NewHost(modules ...Module) (*Host, error) {
	byName := make(map[string]Module, len(modules))
	for _, m := range modules {
		if _, dup := byName[m.Name()]; dup {
			return nil, fmt.Errorf("module %q registered twice", m.Name())
		}
		byName[m.Name()] = m
	}
	for _, m := range modules {
		for _, dep := range m.Dependencies() {
			if _, ok := byName[dep]; !ok {
				return nil, fmt.Errorf("module %q depends on unknown module %q", m.Name(), dep)
			}
		}
	}

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic order among independent modules

	const (
		unvisited = iota
		visiting
		done
	)
	state := make(map[string]int, len(byName))
	ordered := make([]Module, 0, len(byName))
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("module dependency cycle: %v", append(path, name))
		}
		state[name] = visiting
		deps := append([]string(nil), byName[name].Dependencies()...)
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = done
		ordered = append(ordered, byName[name])
		return nil
	}
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return &Host{ordered: ordered}, nil
}

// Modules returns the modules in dependency order.
func (h *Host) Modules() []Module { return append([]Module(nil), h.ordered...) }

// Start starts modules in dependency order. On failure, already started modules are stopped.
func (h *Host) Start(ctx context.Context) error {
	for i, m := range h.ordered {
		if s, ok := m.(Starter); ok {
			if err := s.Start(ctx); err != nil {
				h.started = i
				return errors.Join(fmt.Errorf("starting module %q: %w", m.Name(), err), h.Stop(ctx))
			}
		}
	}
	h.started = len(h.ordered)
	return nil
}

// Stop stops started modules in reverse dependency order.
func (h *Host) Stop(ctx context.Context) error {
	var errs []error
	for i := h.started - 1; i >= 0; i-- {
		if s, ok := h.ordered[i].(Stopper); ok {
			if err := s.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("stopping module %q: %w", h.ordered[i].Name(), err))
			}
		}
	}
	h.started = 0
	return errors.Join(errs...)
}
