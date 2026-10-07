// Package application holds the Imports use cases: previewing and executing a run of a source,
// and reading the runs and the references they leave.
package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/imports/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Registry holds the sources the host offers and the loaders it wrote for each kind of record.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]domain.Source
	loaders map[string]domain.Loader
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{sources: map[string]domain.Source{}, loaders: map[string]domain.Loader{}}
}

// Offer adds sources; a source with the key of another replaces it.
func (r *Registry) Offer(sources ...domain.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range sources {
		r.sources[s.Key()] = s
	}
}

// Load adds loaders; a loader of the kind of another replaces it.
func (r *Registry) Load(loaders ...domain.Loader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range loaders {
		r.loaders[l.Kind()] = l
	}
}

func (r *Registry) source(key string) (domain.Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[key]
	return s, ok
}

func (r *Registry) loader(kind string) domain.Loader {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaders[kind]
}

func (r *Registry) all() []domain.Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Source, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Runs       domain.RunRepository
	References domain.ReferenceRepository
	Registry   *Registry
	UoW        fw.UnitOfWork
	Recorder   app.EventRecorder
	Audit      app.AuditLog
}

// Commands and queries.
type (
	// FileDTO is a file of a run, its content as text.
	FileDTO struct {
		Role    string `json:"role"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	// RunImport previews or executes a source over its files.
	RunImport struct {
		Source string    `json:"source"`
		Files  []FileDTO `json:"files"`
	}
	// CloseStale fails the runs that have been running longer than a run can take: the process
	// that carried them died.
	CloseStale struct {
		OlderThanMinutes int `json:"olderThanMinutes"`
	}
	// GetRun reads a run with its messages.
	GetRun struct{ ID domain.RunID }
	// SearchRuns lists runs, the latest first.
	SearchRuns struct {
		Source, Status string
		Page, Size     int
	}
	// SearchReferences lists what the keys of a source became, or the keys an entity has.
	SearchReferences struct {
		Source, Kind, Scope, Key, Entity string
		Page, Size                       int
	}
	// ListSources lists the sources the host offers.
	ListSources struct{}
)

// DTOs.
type (
	CountDTO struct {
		Kind      string `json:"kind"`
		Read      int    `json:"read"`
		Created   int    `json:"created"`
		Updated   int    `json:"updated"`
		Unchanged int    `json:"unchanged"`
		Skipped   int    `json:"skipped"`
		Failed    int    `json:"failed"`
	}
	MessageDTO struct {
		Severity string `json:"severity"`
		Code     string `json:"code,omitempty"`
		Text     string `json:"text"`
		File     string `json:"file,omitempty"`
		Line     int    `json:"line,omitempty"`
		Kind     string `json:"kind,omitempty"`
		Key      string `json:"key,omitempty"`
		Entity   string `json:"entity,omitempty"`
	}
	// ReportDTO is what a preview answers: what executing would do. Created is what would be
	// created and Unchanged what already exists.
	ReportDTO struct {
		Source          string       `json:"source"`
		Counts          []CountDTO   `json:"counts"`
		Messages        []MessageDTO `json:"messages"`
		Errors          int          `json:"errors"`
		Warnings        int          `json:"warnings"`
		DroppedMessages int          `json:"droppedMessages,omitempty"`
	}
	RunDTO struct {
		ID              string       `json:"id"`
		Version         int64        `json:"version"`
		Source          string       `json:"source"`
		Files           []string     `json:"files"`
		Status          string       `json:"status"`
		StartedAt       string       `json:"startedAt"`
		FinishedAt      string       `json:"finishedAt,omitempty"`
		StartedBy       string       `json:"startedBy,omitempty"`
		Reason          string       `json:"reason,omitempty"`
		Counts          []CountDTO   `json:"counts"`
		Messages        []MessageDTO `json:"messages,omitempty"`
		Errors          int          `json:"errors"`
		Warnings        int          `json:"warnings"`
		DroppedMessages int          `json:"droppedMessages,omitempty"`
	}
	ReferenceDTO struct {
		Source     string `json:"source"`
		Kind       string `json:"kind"`
		Scope      string `json:"scope"`
		Key        string `json:"key"`
		EntityType string `json:"entityType"`
		EntityID   string `json:"entityId"`
		FirstRun   string `json:"firstRun"`
		LastRun    string `json:"lastRun"`
	}
	FileSpecDTO struct {
		Role     string `json:"role"`
		Required bool   `json:"required"`
		About    string `json:"about,omitempty"`
	}
	SourceDTO struct {
		Key   string        `json:"key"`
		Files []FileSpecDTO `json:"files"`
		Kinds []string      `json:"kinds"`
		// Unloaded lists the kinds no loader of the host takes: their records would be skipped.
		Unloaded []string `json:"unloaded"`
	}
	ClosedDTO struct {
		Runs []string `json:"runs"`
	}
)

// Service exposes the use cases.
type Service struct {
	Preview    app.CommandHandler[RunImport, ReportDTO]
	Execute    app.CommandHandler[RunImport, RunDTO]
	CloseStale app.CommandHandler[CloseStale, ClosedDTO]
	GetRun     app.QueryHandler[GetRun, RunDTO]
	SearchRuns app.QueryHandler[SearchRuns, fw.Page[RunDTO]]
	References app.QueryHandler[SearchReferences, fw.Page[ReferenceDTO]]
	Sources    app.QueryHandler[ListSources, []SourceDTO]
}

type service struct {
	Deps
	runs       *orchestration.Orchestrator[domain.RunID, *domain.Run]
	references *orchestration.Orchestrator[domain.ReferenceID, *domain.Reference]
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), pipeline.RequirePermission[In, Out](p))
}

func countDTOs(cs []domain.Count) []CountDTO {
	out := []CountDTO{}
	for _, c := range cs {
		out = append(out, CountDTO(c))
	}
	return out
}

func messageDTOs(ms []domain.Message) []MessageDTO {
	out := []MessageDTO{}
	for _, m := range ms {
		out = append(out, MessageDTO{Severity: m.Severity, Code: m.Code, Text: m.Text, File: m.File, Line: m.Line, Kind: m.Kind, Key: m.Key, Entity: m.Entity})
	}
	return out
}

func runDTO(r *domain.Run, messages bool) RunDTO {
	s := r.State()
	d := RunDTO{ID: r.ID().String(), Version: r.Version(), Source: s.Source, Files: s.Files, Status: string(s.Status), StartedAt: stamp(s.StartedAt),
		FinishedAt: stamp(s.FinishedAt), StartedBy: s.StartedByName, Reason: s.Reason, Counts: countDTOs(s.Counts), Errors: s.Errors, Warnings: s.Warnings,
		DroppedMessages: s.Dropped}
	if d.Files == nil {
		d.Files = []string{}
	}
	if messages {
		d.Messages = messageDTOs(s.Messages)
	}
	return d
}

// refs is what the keys of the running source are, with what the run adds.
type refs struct {
	byKey map[string]*domain.Reference
	ids   map[string]string
}

func refKey(kind, scope, key string) string {
	if scope == "" {
		scope = domain.GlobalScope
	}
	return kind + "\x00" + scope + "\x00" + key
}

func (r *refs) Lookup(kind, scope, key string) (string, bool) {
	id, ok := r.ids[refKey(kind, scope, key)]
	return id, ok
}

func (s service) refsOf(ctx context.Context, source string) (*refs, error) {
	found, err := s.References.Find(ctx, domain.RefFieldSource.Eq(source))
	if err != nil {
		return nil, err
	}
	out := &refs{byKey: map[string]*domain.Reference{}, ids: map[string]string{}}
	for _, f := range found {
		st := f.State()
		k := refKey(st.Kind, st.Scope, st.Key)
		out.byKey[k], out.ids[k] = f, st.EntityID
	}
	return out, nil
}

// read validates the command and lets the source parse the files.
func (s service) read(c RunImport) (domain.Source, []domain.Record, []domain.Message, error) {
	var v fw.Validation
	src, ok := s.Registry.source(strings.ToLower(strings.TrimSpace(c.Source)))
	v.Require(ok, "source", "unknown", "a source the host offers")
	if err := v.Err(); err != nil {
		return nil, nil, nil, err
	}
	size, files, roles := 0, []domain.File{}, map[string]bool{}
	for _, f := range c.Files {
		role := strings.TrimSpace(f.Role)
		known := slices.ContainsFunc(src.Files(), func(fs domain.FileSpec) bool { return fs.Role == role })
		v.Require(known && !roles[role], "files", "role", "each file once, with a role the source takes")
		roles[role] = true
		size += len(f.Content)
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = role
		}
		files = append(files, domain.File{Role: role, Name: name, Content: f.Content})
	}
	for _, fs := range src.Files() {
		v.Require(!fs.Required || roles[fs.Role], "files", "required", "the file "+fs.Role+" is required")
	}
	v.Require(size <= domain.MaxFileBytes, "files", "size", "the files of a run add up to 5 MB at most")
	if err := v.Err(); err != nil {
		return nil, nil, nil, err
	}
	records, messages, err := src.Read(files)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(records) > domain.MaxRecords {
		return nil, nil, nil, fw.Violation("imports.too_many_records", fmt.Sprintf("a run takes %d records at most", domain.MaxRecords))
	}
	return src, records, messages, nil
}

func code(err error) string {
	var rv *fw.RuleViolationError
	switch {
	case errors.As(err, &rv):
		return rv.Code
	case errors.Is(err, fw.ErrForbidden):
		return "imports.forbidden"
	case errors.Is(err, fw.ErrNotFound):
		return "imports.not_found"
	case errors.Is(err, fw.ErrValidation):
		return "imports.invalid"
	}
	return "imports.failed"
}

// process applies the records kind by kind, in the order the source asks for. Each record is on
// its own: one that fails is reported and the rest go on. With a zero run nothing is written.
func (s service) process(ctx context.Context, src domain.Source, records []domain.Record, messages []domain.Message, run domain.RunID, rep *domain.Report) error {
	known, err := s.refsOf(ctx, src.Key())
	if err != nil {
		return err
	}
	for _, m := range messages {
		rep.Add(m)
	}
	kinds := src.Kinds()
	for _, r := range records { // a kind the source did not announce still shows up, at the end
		if !slices.Contains(kinds, r.Kind) {
			kinds = append(kinds, r.Kind)
		}
	}
	for _, kind := range kinds {
		loader := s.Registry.loader(kind)
		missing := false
		rep.Of(kind) // a kind without records still shows, with nothing read
		for _, r := range records {
			if r.Kind != kind {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			count := rep.Of(kind)
			count.Read++
			if loader == nil {
				count.Skipped++
				if !missing {
					missing = true
					rep.Add(domain.Message{Severity: domain.SeverityWarning, Code: "imports.no_loader", Kind: kind,
						Text: "nothing in this installation takes records of this kind: they are skipped"})
				}
				continue
			}
			rctx := ctx
			if !run.IsZero() {
				rctx = app.WithImportProvenance(ctx, app.ImportProvenance{SourceKey: src.Key(), RunID: run.UUID, SourceFile: r.File})
			}
			k := refKey(r.Kind, r.Scope, r.Key)
			id, linked := known.ids[k]
			if !linked {
				if id, err = loader.Find(rctx, r, known); err != nil {
					count.Failed++
					rep.Fail(r, code(err), err.Error())
					continue
				}
			}
			if run.IsZero() { // preview
				if id == "" {
					count.Created++
				} else {
					count.Unchanged++
				}
				continue
			}
			entity, outcome, err := loader.Apply(rctx, r, id, known)
			if err == nil && entity == "" {
				err = errors.New("the loader gave no identity")
			}
			if err != nil {
				count.Failed++
				rep.Fail(r, code(err), err.Error())
				continue
			}
			switch outcome {
			case domain.Created:
				count.Created++
			case domain.Updated:
				count.Updated++
			default:
				outcome = domain.Unchanged
				count.Unchanged++
			}
			known.ids[k] = entity
			if err := s.link(rctx, src.Key(), loader, r, entity, outcome, run, known.byKey[k]); err != nil {
				rep.Add(domain.Message{Severity: domain.SeverityWarning, Code: "imports.reference_not_linked", File: r.File, Line: r.Line, Kind: r.Kind, Key: r.Key,
					Entity: entity, Text: "the record was applied but its reference was not kept (" + err.Error() + "); the next run will look for it by its natural key"})
			}
		}
	}
	return nil
}

// link keeps the reference of a record: a new one, or the run that last changed the entity.
func (s service) link(ctx context.Context, source string, l domain.Loader, r domain.Record, entity string, outcome domain.Outcome, run domain.RunID,
	ref *domain.Reference) error {
	if ref == nil {
		n, err := domain.Link(domain.NewReferenceID(), source, r, l.EntityType(), entity, run)
		if err != nil {
			return err
		}
		return s.references.Create(ctx, n)
	}
	if outcome == domain.Unchanged {
		return nil
	}
	_, err := s.references.Update(ctx, ref.ID(), func(_ context.Context, x *domain.Reference) error {
		x.Touched(run)
		return nil
	})
	return err
}

// visible returns the run when the caller may see it: its own, or any for a global administrator.
func visible(ctx context.Context, r *domain.Run) error {
	ac, ok := authz.FromContext(ctx)
	if ok && (ac.GlobalAdmin || ac.Subject.String() == r.State().StartedBy) {
		return nil
	}
	return fw.NotFound(domain.RunKind, r.ID())
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	if d.Registry == nil {
		d.Registry = NewRegistry()
	}
	s := service{Deps: d, runs: orchestration.New[domain.RunID, *domain.Run](d.Runs, d.UoW, opts...),
		references: orchestration.New[domain.ReferenceID, *domain.Reference](d.References, d.UoW, opts...)}
	svc := &Service{}

	svc.Preview = guard(PermRunExecute, func(ctx context.Context, c RunImport) (ReportDTO, error) {
		src, records, messages, err := s.read(c)
		if err != nil {
			return ReportDTO{}, err
		}
		var rep domain.Report
		if err := s.process(ctx, src, records, messages, domain.RunID{}, &rep); err != nil {
			return ReportDTO{}, err
		}
		return ReportDTO{Source: src.Key(), Counts: countDTOs(rep.Counts), Messages: messageDTOs(rep.Messages), Errors: rep.Errors, Warnings: rep.Warnings,
			DroppedMessages: rep.Dropped}, nil
	})

	svc.Execute = guard(PermRunExecute, func(ctx context.Context, c RunImport) (out RunDTO, err error) {
		src, records, messages, err := s.read(c)
		if err != nil {
			return RunDTO{}, err
		}
		running, err := d.Runs.Find(ctx, spec.And(domain.RunFieldSource.Eq(src.Key()), domain.RunFieldStatus.Eq(string(domain.Running))))
		if err != nil {
			return RunDTO{}, err
		}
		if len(running) > 0 {
			return RunDTO{}, fw.Violation("imports.run_in_progress", "a run of "+src.Key()+" is in progress since "+stamp(running[0].State().StartedAt))
		}
		by, name := "", ""
		if ac, ok := authz.FromContext(ctx); ok {
			by, name = ac.Subject.String(), ac.SubjectName
		}
		names := []string{}
		for _, f := range c.Files {
			names = append(names, cmp(f.Name, f.Role))
		}
		run, err := domain.StartRun(domain.NewRunID(), src.Key(), names, by, name, fw.Now())
		if err != nil {
			return RunDTO{}, err
		}
		if err := s.runs.Create(ctx, run); err != nil {
			return RunDTO{}, err
		}
		// Whatever happens from here on, the run is closed: the caller going away or a loader
		// breaking leaves it failed, with what it did until then.
		var rep domain.Report
		closing := context.WithoutCancel(ctx)
		failure := ""
		func() {
			defer func() {
				if p := recover(); p != nil {
					failure = fmt.Sprint("a loader broke: ", p)
				}
			}()
			if err := s.process(ctx, src, records, messages, run.ID(), &rep); err != nil {
				failure = err.Error()
			}
		}()
		done, err := s.runs.Update(closing, run.ID(), func(_ context.Context, r *domain.Run) error {
			if failure != "" {
				r.Abort(rep, failure, fw.Now())
				return nil
			}
			return r.Finish(rep, fw.Now())
		})
		if err != nil {
			// The result could not be kept: at least the run does not stay running.
			cause := err
			if _, err := s.runs.Update(closing, run.ID(), func(_ context.Context, r *domain.Run) error {
				r.Abort(domain.Report{}, "the result of the run could not be recorded", fw.Now())
				return nil
			}); err != nil {
				return RunDTO{}, errors.Join(cause, err)
			}
			return RunDTO{}, cause
		}
		return runDTO(done, true), nil
	})

	svc.CloseStale = guard(PermRunExecute, func(ctx context.Context, c CloseStale) (ClosedDTO, error) {
		var v fw.Validation
		if c.OlderThanMinutes == 0 {
			c.OlderThanMinutes = 60
		}
		v.Require(c.OlderThanMinutes >= 5, "olderThanMinutes", "range", "at least five minutes")
		if err := v.Err(); err != nil {
			return ClosedDTO{}, err
		}
		running, err := d.Runs.Find(ctx, domain.RunFieldStatus.Eq(string(domain.Running)))
		if err != nil {
			return ClosedDTO{}, err
		}
		out, now := ClosedDTO{Runs: []string{}}, fw.Now()
		limit := now.Add(-time.Duration(c.OlderThanMinutes) * time.Minute)
		for _, r := range running {
			if r.State().StartedAt.After(limit) {
				continue
			}
			closed := false
			if _, err := s.runs.Update(ctx, r.ID(), func(_ context.Context, x *domain.Run) error {
				closed = x.Abort(domain.Report{}, "the run did not end: the process that carried it stopped", now)
				return nil
			}); err != nil {
				return out, err
			}
			if closed {
				out.Runs = append(out.Runs, r.ID().String())
			}
		}
		return out, nil
	})

	svc.GetRun = guard(PermRunRead, func(ctx context.Context, q GetRun) (RunDTO, error) {
		r, err := d.Runs.Get(ctx, q.ID)
		if err != nil {
			return RunDTO{}, err
		}
		if err := visible(ctx, r); err != nil {
			return RunDTO{}, err
		}
		return runDTO(r, true), nil
	})

	svc.SearchRuns = guard(PermRunRead, func(ctx context.Context, q SearchRuns) (fw.Page[RunDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Run]{}
		if ac, ok := authz.FromContext(ctx); !ok {
			parts = append(parts, spec.None[*domain.Run]())
		} else if !ac.GlobalAdmin {
			parts = append(parts, domain.RunFieldStartedBy.Eq(ac.Subject.String()))
		}
		if q.Source != "" {
			parts = append(parts, domain.RunFieldSource.Eq(strings.ToLower(q.Source)))
		}
		if q.Status != "" {
			v.Require(slices.Contains(domain.Statuses, domain.Status(q.Status)), "status", "enum", "a status")
			parts = append(parts, domain.RunFieldStatus.Eq(q.Status))
		}
		if err := v.Err(); err != nil {
			return fw.Page[RunDTO]{}, err
		}
		page, err := d.Runs.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.RunFieldStartedAt.Desc()))
		if err != nil {
			return fw.Page[RunDTO]{}, err
		}
		return fw.MapPage(page, func(r *domain.Run) RunDTO { return runDTO(r, false) }), nil
	})

	svc.References = guard(PermReferenceRead, func(ctx context.Context, q SearchReferences) (fw.Page[ReferenceDTO], error) {
		parts := []spec.Specification[*domain.Reference]{}
		if q.Source != "" {
			parts = append(parts, domain.RefFieldSource.Eq(strings.ToLower(q.Source)))
		}
		if q.Kind != "" {
			parts = append(parts, domain.RefFieldKind.Eq(q.Kind))
		}
		if q.Scope != "" {
			parts = append(parts, domain.RefFieldScope.Eq(q.Scope))
		}
		if q.Key != "" {
			parts = append(parts, domain.RefFieldKey.Eq(q.Key))
		}
		if q.Entity != "" {
			parts = append(parts, domain.RefFieldEntity.Eq(q.Entity))
		}
		page, err := d.References.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.RefFieldSource.Asc(), domain.RefFieldKind.Asc(),
			domain.RefFieldScope.Asc(), domain.RefFieldKey.Asc()))
		if err != nil {
			return fw.Page[ReferenceDTO]{}, err
		}
		return fw.MapPage(page, func(r *domain.Reference) ReferenceDTO {
			st := r.State()
			return ReferenceDTO{Source: st.Source, Kind: st.Kind, Scope: st.Scope, Key: st.Key, EntityType: st.EntityType, EntityID: st.EntityID,
				FirstRun: st.FirstRun.String(), LastRun: st.LastRun.String()}
		}), nil
	})

	svc.Sources = guard(PermRunRead, func(_ context.Context, _ ListSources) ([]SourceDTO, error) {
		out := []SourceDTO{}
		for _, src := range d.Registry.all() {
			dto := SourceDTO{Key: src.Key(), Files: []FileSpecDTO{}, Kinds: src.Kinds(), Unloaded: []string{}}
			for _, f := range src.Files() {
				dto.Files = append(dto.Files, FileSpecDTO(f))
			}
			for _, k := range src.Kinds() {
				if d.Registry.loader(k) == nil {
					dto.Unloaded = append(dto.Unloaded, k)
				}
			}
			out = append(out, dto)
		}
		return out, nil
	})
	return svc
}

func cmp(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.RunFinished) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.RunFinishedV1{RunID: e.AggregateID, Source: e.Source, Status: e.Status, StartedAt: stamp(e.StartedAt),
			FinishedAt: stamp(e.FinishedAt), Errors: e.Errors, Warnings: e.Warnings, Created: e.Created, Updated: e.Updated}}, nil
	})
	return r
}
