// Package application contiene las consultas de Audit: el historial de un agregado de cualquier
// contexto delimitado, leído del registro de auditoría que ese contexto ya lleva con cada cambio.
// Audit no guarda nada propio y no conoce ningún otro contexto: el anfitrión registra, para cada
// tipo de agregado, dónde está su registro y cómo saber si quien llama puede ver el agregado.
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

// PermTrailRead lee el historial de un agregado (los endpoints de auditoría del C# sólo exigían
// autenticación). Se necesita además de poder ver el propio agregado.
var PermTrailRead = authz.MustPermission("Audit.Trail.Read")

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission { return []authz.Permission{PermTrailRead} }

// Guard dice si quien llama puede ver un agregado: devuelve el error que da el contexto dueño
// cuando no puede (404 fuera de sus empresas, 403 sin su permiso).
type Guard func(ctx context.Context, id string) error

// Seeing construye una guarda a partir de la consulta que un contexto ya tiene para cargar un
// agregado: quien puede cargarlo puede leer su historial.
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

// Registry es lo que el anfitrión le dice a Audit: el registro y la guarda de cada tipo de agregado.
type Registry struct {
	mu       sync.RWMutex
	subjects map[string]subject
}

// NewRegistry devuelve un registro vacío.
func NewRegistry() *Registry { return &Registry{subjects: map[string]subject{}} }

// Register declara un tipo de agregado (su nombre de tipo estable, como "billing.invoice"), el
// registro de auditoría de su contexto y su guarda. Sin guarda, sólo un administrador global lee
// su historial.
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

// Service expone las consultas.
type Service struct {
	Trail app.QueryHandler[GetTrail, []EntryDTO]
	Types app.QueryHandler[ListTypes, []TypeDTO]
}

// GetTrail pide el historial de un agregado, lo más antiguo primero.
type GetTrail struct{ Type, ID string }

// ListTypes lista los tipos de agregado con historial.
type ListTypes struct{}

// TypeDTO es un tipo de agregado con historial. Los restringidos sólo los lee un administrador
// global.
type TypeDTO struct {
	Type       string `json:"type"`
	Context    string `json:"context"`
	Restricted bool   `json:"restricted"`
}

// ChangeDTO es un campo que cambió.
type ChangeDTO struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
}

// EntryDTO es un cambio de un agregado: quién, cuándo, cómo, qué campos y qué eventos.
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
		e.Import = r.Import.SourceKey // el origen de la importación que hizo el cambio
	}
	for _, c := range r.Changes {
		e.Changes = append(e.Changes, ChangeDTO{Field: c.Field, Old: c.Old, New: c.New})
	}
	return e
}

// NewService conecta las consultas sobre un registro.
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
