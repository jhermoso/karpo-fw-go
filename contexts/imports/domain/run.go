package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Status is how a run is or ended.
type Status string

// Statuses. The C# never wrote "failed": a run that broke stayed "running" for ever.
const (
	Running             Status = "running"
	Succeeded           Status = "succeeded"
	CompletedWithErrors Status = "completed-with-errors"
	Failed              Status = "failed"
)

// Statuses lists the valid statuses.
var Statuses = []Status{Running, Succeeded, CompletedWithErrors, Failed}

// RunState is the persisted state of a run.
type RunState struct {
	Source        string
	Files         []string // names, as they were sent
	Status        Status
	StartedAt     time.Time
	FinishedAt    time.Time
	StartedBy     string // subject that launched it
	StartedByName string
	Reason        string // why it failed
	Counts        []Count
	Messages      []Message
	Errors        int
	Warnings      int
	Dropped       int // messages beyond MaxMessages
	Audit         traits.AuditStamp
}

// Run is one execution of an import: what was read, what it became and what went wrong.
type Run struct {
	fw.BaseAggregateRoot[RunID]
	traits.Audited
	s RunState
}

// ValidKey reports whether s can name a source, a kind or a role: lower case letters, digits,
// dashes and underscores.
func ValidKey(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ReconstituteRun rebuilds a run.
func ReconstituteRun(id RunID, s RunState) (*Run, error) {
	base, err := fw.NewBaseAggregateRoot(RunKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(ValidKey(s.Source, 64), "source", "format", "a source key")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(!s.StartedAt.IsZero(), "startedAt", "required", "a run has the moment it started")
	v.Require((s.Status == Running) == s.FinishedAt.IsZero(), "finishedAt", "state", "only a run that ended has the moment it did")
	v.Require(len(s.Messages) <= MaxMessages, "messages", "range", "too many messages")
	v.Require(utf8.RuneCountInString(strings.Join(s.Files, "|")) <= 1000, "files", "length", "file names too long")
	for _, f := range s.Files {
		v.Require(f != "" && !strings.Contains(f, "|"), "files", "format", "a file name")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Files, s.Counts, s.Messages = slices.Clone(s.Files), slices.Clone(s.Counts), slices.Clone(s.Messages)
	return &Run{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// StartRun opens a run.
func StartRun(id RunID, source string, files []string, startedBy, startedByName string, now time.Time) (*Run, error) {
	names := []string{}
	for _, f := range files {
		if f = strings.ReplaceAll(clip(f, 200), "|", "_"); f != "" {
			names = append(names, f)
		}
	}
	return ReconstituteRun(id, RunState{Source: source, Files: names, Status: Running, StartedAt: now.UTC(), StartedBy: clip(startedBy, 64),
		StartedByName: clip(startedByName, 200)})
}

// State returns the state (slices are copies).
func (r *Run) State() RunState {
	s := r.s
	s.Files, s.Counts, s.Messages = slices.Clone(s.Files), slices.Clone(s.Counts), slices.Clone(s.Messages)
	return s
}

func (r *Run) end(status Status, now time.Time) {
	r.s.Status, r.s.FinishedAt = status, now.UTC()
	created, updated := 0, 0
	for _, c := range r.s.Counts {
		created, updated = created+c.Created, updated+c.Updated
	}
	r.Raise(RunFinished{EventMeta: r.NewEventMeta(), Source: r.s.Source, Status: string(status), StartedAt: r.s.StartedAt, FinishedAt: r.s.FinishedAt,
		Errors: r.s.Errors, Warnings: r.s.Warnings, Created: created, Updated: updated})
}

func (r *Run) take(rep Report) {
	r.s.Counts, r.s.Messages = slices.Clone(rep.Counts), slices.Clone(rep.Messages)
	r.s.Errors, r.s.Warnings, r.s.Dropped = rep.Errors, rep.Warnings, rep.Dropped
}

// Finish closes a run that went through all its records: with errors or without them. A run
// someone gave up for dead while it was still going stays failed, but keeps what it did.
func (r *Run) Finish(rep Report, now time.Time) error {
	if r.s.Status == Failed {
		r.take(rep)
		return nil
	}
	if r.s.Status != Running {
		return fw.Violation("imports.run_closed", "the run already ended "+string(r.s.Status))
	}
	r.take(rep)
	if rep.Errors > 0 {
		r.end(CompletedWithErrors, now)
	} else {
		r.end(Succeeded, now)
	}
	return nil
}

// Abort closes a run that could not go on, keeping what it did until then. It reports whether it
// changed anything: a run that ended stays as it is.
func (r *Run) Abort(rep Report, reason string, now time.Time) bool {
	if r.s.Status != Running {
		return false
	}
	if len(rep.Counts) > 0 || len(rep.Messages) > 0 {
		r.take(rep)
	}
	r.s.Reason = clip(reason, 500)
	r.end(Failed, now)
	return true
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Run) AuditSnapshot() map[string]any {
	return map[string]any{"source": r.s.Source, "status": string(r.s.Status), "errors": r.s.Errors, "warnings": r.s.Warnings}
}

// Run fields.
var (
	RunFieldSource    = spec.Comparable("source_key", func(r *Run) string { return r.s.Source })
	RunFieldStatus    = spec.Comparable("status", func(r *Run) string { return string(r.s.Status) })
	RunFieldStartedBy = spec.Comparable("started_by", func(r *Run) string { return r.s.StartedBy })
	RunFieldStartedAt = spec.OrderedBy("started_at", func(r *Run) time.Time { return r.s.StartedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// RunFinished is raised when a run ends, however it does.
type RunFinished struct {
	fw.EventMeta
	Source     string    `json:"source"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Errors     int       `json:"errors"`
	Warnings   int       `json:"warnings"`
	Created    int       `json:"created"`
	Updated    int       `json:"updated"`
}

// EventType implements fw.DomainEvent.
func (RunFinished) EventType() string { return "imports.run_finished" }

// ReferenceState is the persisted state of a reference.
type ReferenceState struct {
	Source     string
	Kind       string
	Scope      string
	Key        string
	EntityType string
	EntityID   string
	FirstRun   RunID
	LastRun    RunID // the last run that created or changed the entity
	Audit      traits.AuditStamp
}

// Reference remembers which entity a key of a source became (the C# legacy_reference), so the
// next run finds it even if its name changed.
type Reference struct {
	fw.BaseAggregateRoot[ReferenceID]
	traits.Audited
	s ReferenceState
}

// ReconstituteReference rebuilds a reference.
func ReconstituteReference(id ReferenceID, s ReferenceState) (*Reference, error) {
	base, err := fw.NewBaseAggregateRoot(ReferenceKind, id)
	if err != nil {
		return nil, err
	}
	s.Scope, s.Key, s.EntityType, s.EntityID = strings.TrimSpace(s.Scope), strings.TrimSpace(s.Key), strings.TrimSpace(s.EntityType), strings.TrimSpace(s.EntityID)
	if s.Scope == "" {
		s.Scope = GlobalScope
	}
	var v fw.Validation
	v.Require(ValidKey(s.Source, 64), "source", "format", "a source key")
	v.Require(ValidKey(s.Kind, 64), "kind", "format", "a kind of record")
	v.Require(utf8.RuneCountInString(s.Scope) <= 128, "scope", "length", "a scope of at most 128 characters")
	v.Require(s.Key != "" && utf8.RuneCountInString(s.Key) <= 256, "key", "length", "a key of 1 to 256 characters")
	v.Require(s.EntityType != "" && utf8.RuneCountInString(s.EntityType) <= 128, "entityType", "length", "an entity type")
	v.Require(s.EntityID != "" && utf8.RuneCountInString(s.EntityID) <= 128, "entityId", "length", "an entity identity")
	v.Require(!s.FirstRun.IsZero() && !s.LastRun.IsZero(), "run", "required", "the run that linked it")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Reference{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Link remembers that a key of a source is an entity, as of a run.
func Link(id ReferenceID, source string, r Record, entityType, entityID string, run RunID) (*Reference, error) {
	return ReconstituteReference(id, ReferenceState{Source: source, Kind: r.Kind, Scope: r.Scope, Key: r.Key, EntityType: entityType, EntityID: entityID,
		FirstRun: run, LastRun: run})
}

// State returns the state.
func (r *Reference) State() ReferenceState { return r.s }

// Touched notes the run that last created or changed the entity.
func (r *Reference) Touched(run RunID) { r.s.LastRun = run }

// AuditSnapshot implements traits.Snapshotter.
func (r *Reference) AuditSnapshot() map[string]any {
	return map[string]any{"source": r.s.Source, "kind": r.s.Kind, "scope": r.s.Scope, "key": r.s.Key, "entity": r.s.EntityID, "lastRun": r.s.LastRun.String()}
}

// Reference fields.
var (
	RefFieldSource = spec.Ordered("source_key", func(r *Reference) string { return r.s.Source })
	RefFieldKind   = spec.Ordered("record_kind", func(r *Reference) string { return r.s.Kind })
	RefFieldScope  = spec.Ordered("scope_key", func(r *Reference) string { return r.s.Scope })
	RefFieldKey    = spec.Ordered("legacy_key", func(r *Reference) string { return r.s.Key })
	RefFieldEntity = spec.Comparable("entity_id", func(r *Reference) string { return r.s.EntityID })
)

// Repositories.
type (
	RunRepository       = fw.Repository[RunID, *Run]
	ReferenceRepository = fw.Repository[ReferenceID, *Reference]
)
