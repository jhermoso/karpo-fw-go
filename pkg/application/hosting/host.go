// Package hosting runs bounded-context modules (application.Module) in dependency order.
package hosting

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

// Host validates module dependencies and runs their lifecycle in dependency order.
type Host struct {
	ordered []application.Module
	started int
}

// NewHost orders modules so every module comes after its dependencies. It fails on duplicate
// names, unknown dependencies and cycles.
func NewHost(modules ...application.Module) (*Host, error) {
	byName := make(map[string]application.Module, len(modules))
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
	ordered := make([]application.Module, 0, len(byName))
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
func (h *Host) Modules() []application.Module { return append([]application.Module(nil), h.ordered...) }

// Start starts modules in dependency order. On failure, already started modules are stopped.
func (h *Host) Start(ctx context.Context) error {
	for i, m := range h.ordered {
		if s, ok := m.(application.Starter); ok {
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
		if s, ok := h.ordered[i].(application.Stopper); ok {
			if err := s.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("stopping module %q: %w", h.ordered[i].Name(), err))
			}
		}
	}
	h.started = 0
	return errors.Join(errs...)
}
