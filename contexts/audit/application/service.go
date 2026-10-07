// Package application holds the Audit queries: the history of an aggregate of any bounded
// context, read from the audit log that context already keeps with each change. Audit stores
// nothing of its own and knows no other context: the host registers, for each kind of aggregate,
// where its log is and how to tell whether the caller may see the aggregate.
package application

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PermTrailRead reads the history of an aggregate (the C# audit endpoints required
// authentication only). It is needed besides being able to see the aggregate itself.
var PermTrailRead = authz.MustPermission("Audit.Trail.Read")

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission { return []authz.Permission{PermTrailRead} }

// Guard tells whether the caller may see an aggregate: it returns the error the owning context
// gives when they may not (404 outside their companies, 403 without its permission).
type Guard func(ctx context.Context, id string) error

// Seeing builds a guard from the query a context already has to load an aggregate: who can load
// it can read its history.
func Seeing[ID, Q, Out any](parse func(string) (ID, error), query func(ID) Q, get app.QueryHandler[Q, Out]) Guard {
	return func(ctx context.Context, id string) error {
		parsed, err := parse(id)
		if err != nil {
			return fmt.Errorf("%w: invalid id", fw.ErrValidation)
		}
		_, err = get.Handle(ctx, query(parsed))
		return err
	}
}

type subject struct {
	log   app.AuditLog
	guard Guard
}

// Registry is what the host tells Audit: the log and the guard of each kind of aggregate.
type Registry struct {
	mu       sync.RWMutex
	subjects map[string]subject
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{subjects: map[string]subject{}} }

// Register declares a kind of aggregate (its stable type name, such as "billing.invoice"), the
// audit log of its context and its guard. Without a guard only a global administrator reads its
// history.
func (r *Registry) Register(aggregateType string, log app.AuditLog, guard Guard) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subjects[aggregateType] = subject{log: log, guard: guard}
	return r
}

func (r *Registry) get(aggregateType string) (subject, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.subjects[aggregateType]
	return s, ok
}

func (r *Registry) types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.subjects))
	for t := range r.subjects {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Service exposes the queries.
type Service struct {
	Trail app.QueryHandler[GetTrail, []EntryDTO]
	Types app.QueryHandler[ListTypes, []TypeDTO]
}

// GetTrail asks for the history of an aggregate, oldest first.
type GetTrail struct{ Type, ID string }

// ListTypes lists the kinds of aggregate with a history.
type ListTypes struct{}

// TypeDTO is a kind of aggregate with a history. Restricted ones are read by a global
// administrator only.
type TypeDTO struct {
	Type       string `json:"type"`
	Context    string `json:"context"`
	Restricted bool   `json:"restricted"`
}

// ChangeDTO is a field that changed.
type ChangeDTO struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
}

// EntryDTO is a change of an aggregate: who, when, how, which fields and which events.
type EntryDTO struct {
	Version       int64       `json:"version"`
	Operation     string      `json:"operation"` // created | updated | deleted
	At            string      `json:"at"`
	Actor         string      `json:"actor"`
	ActorParty    string      `json:"actorParty,omitempty"`
	Channel       string      `json:"channel,omitempty"`
	Import        string      `json:"import,omitempty"`
	CorrelationID string      `json:"correlationId,omitempty"`
	Changes       []ChangeDTO `json:"changes,omitempty"`
	Events        []string    `json:"events,omitempty"`
}

func entryDTO(r app.AuditRecord) EntryDTO {
	e := EntryDTO{Version: r.AggregateVersion, Operation: r.Operation, At: r.At.UTC().Format(time.RFC3339), Actor: r.Actor.Name, Channel: r.Channel,
		CorrelationID: r.CorrelationID, Events: slices.Clone(r.Events)}
	if !r.Actor.PartyID.IsZero() {
		e.ActorParty = r.Actor.PartyID.String()
	}
	if r.Import != nil {
		e.Import = r.Import.SourceKey // the source of the import run that made the change
	}
	for _, c := range r.Changes {
		e.Changes = append(e.Changes, ChangeDTO{Field: c.Field, Old: c.Old, New: c.New})
	}
	return e
}

// NewService wires the queries on a registry.
func NewService(reg *Registry) *Service {
	guard := func(ctx context.Context, s subject, id string) error {
		if s.guard != nil {
			return s.guard(ctx, id)
		}
		if ac, ok := authz.FromContext(ctx); !ok || !ac.GlobalAdmin {
			return fmt.Errorf("%w: this history is read by a global administrator", fw.ErrForbidden)
		}
		return nil
	}
	svc := &Service{}
	svc.Trail = app.Chain[GetTrail, []EntryDTO](app.HandlerFunc[GetTrail, []EntryDTO](func(ctx context.Context, q GetTrail) ([]EntryDTO, error) {
		var v fw.Validation
		s, known := reg.get(strings.TrimSpace(q.Type))
		v.Require(known, "type", "enum", "a kind of aggregate with a history")
		v.Require(strings.TrimSpace(q.ID) != "" && len(q.ID) <= 64, "id", "required", "the identity of the aggregate")
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := guard(ctx, s, q.ID); err != nil {
			return nil, err
		}
		records, err := s.log.Trail(ctx, strings.TrimSpace(q.Type), strings.TrimSpace(q.ID))
		if err != nil {
			return nil, err
		}
		out := []EntryDTO{}
		for _, r := range records {
			out = append(out, entryDTO(r))
		}
		return out, nil
	}), pipeline.RequirePermission[GetTrail, []EntryDTO](PermTrailRead))
	svc.Types = app.Chain[ListTypes, []TypeDTO](app.HandlerFunc[ListTypes, []TypeDTO](func(context.Context, ListTypes) ([]TypeDTO, error) {
		out := []TypeDTO{}
		for _, t := range reg.types() {
			s, _ := reg.get(t)
			ctxName, _, _ := strings.Cut(t, ".")
			out = append(out, TypeDTO{Type: t, Context: ctxName, Restricted: s.guard == nil})
		}
		return out, nil
	}), pipeline.RequirePermission[ListTypes, []TypeDTO](PermTrailRead))
	return svc
}
