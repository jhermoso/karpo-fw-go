// Package application holds the Exports use cases: asking for a list as a file, writing it with
// the sight of who asked, and handing it over.
package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Permissions (the C# checked the read permission of the list by hand and nothing else). Asking
// for an export also needs the permission of the list itself.
var (
	PermJobRead   = authz.MustPermission("Exports.Job.Read")
	PermJobCreate = authz.MustPermission("Exports.Job.Create")
	PermJobRun    = authz.MustPermission("Exports.Job.Run")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermJobRead, PermJobCreate, PermJobRun}
}

// Defaults of the service.
const (
	DefaultKeep  = 24 * time.Hour   // how long a file waits to be downloaded
	DefaultStuck = 30 * time.Minute // a job writing for longer is given up
	BatchSize    = 2000
	// ProgressEvery is how many pages go by between two notes of the progress of a job.
	ProgressEvery = 10
)

// Dataset is a list that can be exported. The host writes one over the queries of the context
// that owns the list, which apply the scope of the caller: the job calls it as who asked.
type Dataset interface {
	// Key names the list (parties, customers...).
	Key() string
	// Title heads the sheet when the request gives none.
	Title() string
	// Permission is what reading the list requires.
	Permission() authz.Permission
	// Columns are the columns of the file, in order.
	Columns() []domain.Column
	// Filters are the names of the conditions it understands.
	Filters() []string
	// Page returns the rows after a cursor ("" is the start) and the cursor of the next page ("":
	// there are no more).
	Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]domain.Row, string, error)
}

// Files keeps the files of the jobs (a folder, a bucket).
type Files interface {
	Create(ctx context.Context, key string) (io.WriteCloser, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Remove(ctx context.Context, key string) error
}

// Registry holds the lists the host offers.
type Registry struct {
	mu       sync.RWMutex
	datasets map[string]Dataset
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{datasets: map[string]Dataset{}} }

// Offer adds lists; one with the key of another replaces it.
func (r *Registry) Offer(ds ...Dataset) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range ds {
		r.datasets[d.Key()] = d
	}
}

func (r *Registry) get(key string) (Dataset, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.datasets[key]
	return d, ok
}

func (r *Registry) all() []Dataset {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Dataset, 0, len(r.datasets))
	for _, d := range r.datasets {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Deps are the ports the use cases need; Audit is optional, Keep and Stuck have defaults.
type Deps struct {
	Jobs     domain.JobRepository
	Files    Files
	Registry *Registry
	UoW      fw.UnitOfWork
	Audit    app.AuditLog
	Words    domain.Words
	Keep     time.Duration
	Stuck    time.Duration
}

// Commands and queries.
type (
	// StartExport asks for a list as a file.
	StartExport struct {
		Dataset string            `json:"dataset"`
		Format  string            `json:"format,omitempty"` // csv (default) or xlsx
		Title   string            `json:"title,omitempty"`
		Filter  map[string]string `json:"filter,omitempty"`
	}
	// RunNext writes the job that has waited longest (for a worker of the host).
	RunNext struct{}
	// Purge removes the files kept long enough and gives up the jobs that stopped being written.
	Purge struct{}
	// GetJob reads a job.
	GetJob struct{ ID domain.JobID }
	// CancelJob gives up a job that waits or runs.
	CancelJob struct{ ID domain.JobID }
	// DownloadJob opens the file of a completed job.
	DownloadJob struct{ ID domain.JobID }
	// ListJobs lists the jobs of the caller, the latest first; All, those of everybody (global
	// administrators only).
	ListJobs struct {
		All        bool
		Page, Size int
	}
	// ListDatasets lists the lists the caller may export.
	ListDatasets struct{}
)

// DTOs.
type (
	JobDTO struct {
		ID          string `json:"id"`
		Dataset     string `json:"dataset"`
		Format      string `json:"format"`
		Title       string `json:"title,omitempty"`
		Status      string `json:"status"`
		Rows        int    `json:"rows"`
		FileName    string `json:"fileName,omitempty"`
		Size        int64  `json:"size,omitempty"`
		Error       string `json:"error,omitempty"`
		RequestedBy string `json:"requestedBy"`
		RequestedAt string `json:"requestedAt"`
		StartedAt   string `json:"startedAt,omitempty"`
		FinishedAt  string `json:"finishedAt,omitempty"`
		ExpiresAt   string `json:"expiresAt,omitempty"`
		Version     int64  `json:"version"`
	}
	// RanDTO is what a worker did: the job it wrote, if any waited.
	RanDTO struct {
		Job *JobDTO `json:"job,omitempty"`
	}
	PurgedDTO struct {
		Expired []string `json:"expired"`
		GivenUp []string `json:"givenUp"`
	}
	ColumnDTO struct {
		Field  string `json:"field"`
		Header string `json:"header"`
		Type   string `json:"type"`
	}
	DatasetDTO struct {
		Key     string      `json:"key"`
		Title   string      `json:"title"`
		Columns []ColumnDTO `json:"columns"`
		Filters []string    `json:"filters"`
	}
	// File is the file of a job, open: whoever takes it closes it.
	File struct {
		Name        string
		ContentType string
		Size        int64
		Content     io.ReadCloser
	}
)

// Service exposes the use cases.
type Service struct {
	Start    app.CommandHandler[StartExport, JobDTO]
	RunNext  app.CommandHandler[RunNext, RanDTO]
	Purge    app.CommandHandler[Purge, PurgedDTO]
	Cancel   app.CommandHandler[CancelJob, JobDTO]
	Get      app.QueryHandler[GetJob, JobDTO]
	Download app.QueryHandler[DownloadJob, File]
	List     app.QueryHandler[ListJobs, fw.Page[JobDTO]]
	Datasets app.QueryHandler[ListDatasets, []DatasetDTO]
}

type service struct {
	Deps
	jobs *orchestration.Orchestrator[domain.JobID, *domain.Job]
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func jobDTO(j *domain.Job) JobDTO {
	s := j.State()
	return JobDTO{ID: j.ID().String(), Dataset: s.Dataset, Format: s.Format, Title: s.Title, Status: string(s.Status), Rows: s.Rows, FileName: s.FileName,
		Size: s.Size, Error: s.Error, RequestedBy: s.Requester.Name, RequestedAt: stamp(s.RequestedAt), StartedAt: stamp(s.StartedAt),
		FinishedAt: stamp(s.FinishedAt), ExpiresAt: stamp(s.ExpiresAt), Version: j.Version()}
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

// requesterOf captures who asks and what they can see now.
func requesterOf(ctx context.Context) (domain.Requester, bool) {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return domain.Requester{}, false
	}
	r := domain.Requester{Subject: ac.Subject, Name: ac.SubjectName, Kind: string(ac.Kind), Party: ac.ActorPartyID, Global: ac.GlobalAdmin}
	for _, org := range ac.EffectiveOrganizations {
		level := authz.Restricted
		for _, g := range ac.Grants {
			if g.OrganizationID == org {
				level = g.Level
			}
		}
		r.Scope = append(r.Scope, domain.Access{Organization: org, Level: string(level)})
	}
	return r, true
}

// as rebuilds the sight of who asked, with the permission of the list and nothing else.
func as(ctx context.Context, r domain.Requester, p authz.Permission) (context.Context, error) {
	c := authz.Context{Subject: r.Subject, SubjectName: r.Name, Kind: authz.ActorKind(r.Kind), ActorPartyID: r.Party, GlobalAdmin: r.Global,
		Permissions: []authz.Permission{p}, EffectiveOrganizations: []fw.UUID{}}
	for _, a := range r.Scope {
		c.Grants = append(c.Grants, authz.Grant{OrganizationID: a.Organization, Level: authz.ParseAccessLevel(a.Level)})
		c.EffectiveOrganizations = append(c.EffectiveOrganizations, a.Organization)
	}
	ac, err := authz.NewContext(c)
	if err != nil {
		return nil, err
	}
	return authz.WithContext(ctx, ac), nil
}

// visible returns nil when the caller asked for the job or administers everything.
func visible(ctx context.Context, j *domain.Job) error {
	if ac, ok := authz.FromContext(ctx); ok && (ac.GlobalAdmin || j.OwnedBy(ac.Subject)) {
		return nil
	}
	return fw.NotFound(domain.JobKind, j.ID())
}

// update changes a job, again if somebody changed it meanwhile (a cancellation and the progress
// of the worker may meet).
func (s service) update(ctx context.Context, id domain.JobID, fn func(*domain.Job)) (*domain.Job, error) {
	var (
		j   *domain.Job
		err error
	)
	for range 5 {
		if j, err = s.jobs.Update(ctx, id, func(_ context.Context, j *domain.Job) error { fn(j); return nil }); !errors.Is(err, fw.ErrConflict) {
			break
		}
	}
	return j, err
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

var errCancelled = errors.New("cancelled")

// write writes the list of a job into its file and returns the rows and the bytes written.
func (s service) write(ctx context.Context, j *domain.Job, ds Dataset, key string) (int, int64, error) {
	st := j.State()
	rctx, err := as(ctx, st.Requester, ds.Permission())
	if err != nil {
		return 0, 0, err
	}
	out, err := s.Files.Create(ctx, key)
	if err != nil {
		return 0, 0, err
	}
	count := &counter{w: out}
	title := st.Title
	if title == "" {
		title = ds.Title()
	}
	w, err := domain.NewWriter(st.Format, count, title, ds.Columns(), s.Words)
	if err != nil {
		_ = out.Close()
		return 0, 0, err
	}
	filters := map[string]string{}
	for _, f := range st.Filters {
		filters[f.Name] = f.Value
	}
	rows, pages, cursor := 0, 0, ""
	fail := func(err error) (int, int64, error) {
		_ = out.Close()
		return rows, count.n, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		page, next, err := ds.Page(rctx, filters, cursor, BatchSize)
		if err != nil {
			return fail(err)
		}
		if rows+len(page) > domain.MaxRows {
			return fail(fw.Violation("exports.too_many_rows", fmt.Sprintf("the list has more than %d rows: narrow it with a filter", domain.MaxRows)))
		}
		for _, r := range page {
			if err := w.Write(r); err != nil {
				return fail(err)
			}
		}
		rows += len(page)
		if next == "" || next == cursor {
			break
		}
		cursor = next
		// Between pages a job somebody gave up stops; the progress is told every few pages.
		pages++
		now, err := s.Jobs.Get(ctx, j.ID())
		if err != nil {
			return fail(err)
		}
		if now.State().Status != domain.Processing {
			return fail(errCancelled)
		}
		if pages%ProgressEvery == 0 {
			if _, err := s.update(ctx, j.ID(), func(x *domain.Job) { x.Progress(rows) }); err != nil {
				return fail(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		return fail(err)
	}
	return rows, count.n, out.Close()
}

// run writes a job that was just taken and closes it whatever happens.
func (s service) run(ctx context.Context, j *domain.Job) (*domain.Job, error) {
	st := j.State()
	closing := context.WithoutCancel(ctx)
	key := j.ID().String() + "." + st.Format
	give := func(reason string, cause error) (*domain.Job, error) {
		_ = s.Files.Remove(closing, key)
		done, err := s.update(closing, j.ID(), func(x *domain.Job) { x.Fail(reason, fw.Now()) })
		if errors.Is(cause, errCancelled) {
			cause = nil
		}
		return done, errors.Join(cause, err)
	}
	ds, ok := s.Registry.get(st.Dataset)
	if !ok {
		return give("the list is no longer offered", nil)
	}
	var (
		rows int
		size int64
		err  error
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("the list broke: %v", p)
			}
		}()
		rows, size, err = s.write(ctx, j, ds, key)
	}()
	var rv *fw.RuleViolationError
	switch {
	case errors.As(err, &rv): // a rule: its text is for the person who asked
		return give(rv.Error(), nil)
	case err != nil: // anything else stays in the server: the worker gets it, the person does not
		return give("the file could not be written", err)
	}
	name := fmt.Sprintf("export-%s-%s.%s", st.Dataset, fw.Now().UTC().Format("20060102_1504"), st.Format)
	completed := false
	done, err := s.update(closing, j.ID(), func(x *domain.Job) { completed = x.Complete(rows, name, key, size, fw.Now(), s.Keep) })
	if err != nil || !completed {
		_ = s.Files.Remove(closing, key)
	}
	return done, err
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	if d.Registry == nil {
		d.Registry = NewRegistry()
	}
	if d.Keep <= 0 {
		d.Keep = DefaultKeep
	}
	if d.Stuck <= 0 {
		d.Stuck = DefaultStuck
	}
	if d.Words == (domain.Words{}) {
		d.Words = domain.Spanish
	}
	s := service{Deps: d, jobs: orchestration.New[domain.JobID, *domain.Job](d.Jobs, d.UoW, opts...)}
	svc := &Service{}

	svc.Start = guard(PermJobCreate, func(ctx context.Context, c StartExport) (JobDTO, error) {
		var v fw.Validation
		ds, ok := d.Registry.get(strings.ToLower(strings.TrimSpace(c.Dataset)))
		v.Require(ok, "dataset", "unknown", "a list that can be exported")
		format := strings.ToLower(strings.TrimSpace(c.Format))
		if format == "" {
			format = domain.CSV
		}
		v.Require(slices.Contains(domain.Formats, format), "format", "enum", "csv or xlsx")
		if err := v.Err(); err != nil {
			return JobDTO{}, err
		}
		if err := authz.Require(ctx, ds.Permission()); err != nil {
			return JobDTO{}, err
		}
		filters := []domain.Filter{}
		for name, value := range c.Filter {
			v.Require(slices.Contains(ds.Filters(), name), "filter", "unknown", "the list does not filter by "+name)
			if strings.TrimSpace(value) != "" {
				filters = append(filters, domain.Filter{Name: name, Value: strings.TrimSpace(value)})
			}
		}
		if err := v.Err(); err != nil {
			return JobDTO{}, err
		}
		by, _ := requesterOf(ctx)
		active, err := d.Jobs.Find(ctx, spec.And(domain.JobFieldRequester.Eq(by.Subject),
			domain.JobFieldStatus.In(string(domain.Queued), string(domain.Processing))))
		if err != nil {
			return JobDTO{}, err
		}
		if len(active) >= domain.MaxActiveJobs {
			return JobDTO{}, fw.Violation("exports.too_many_jobs", fmt.Sprintf("you already have %d exports waiting: wait for one or cancel it", len(active)))
		}
		j, err := domain.Request(domain.NewJobID(), ds.Key(), format, c.Title, filters, by, fw.Now())
		if err != nil {
			return JobDTO{}, err
		}
		if err := s.jobs.Create(ctx, j); err != nil {
			return JobDTO{}, err
		}
		return jobDTO(j), nil
	})

	svc.RunNext = guard(PermJobRun, func(ctx context.Context, _ RunNext) (RanDTO, error) {
		waiting, err := d.Jobs.Find(ctx, domain.JobFieldStatus.Eq(string(domain.Queued)))
		if err != nil {
			return RanDTO{}, err
		}
		slices.SortFunc(waiting, func(a, b *domain.Job) int {
			if c := a.State().RequestedAt.Compare(b.State().RequestedAt); c != 0 {
				return c
			}
			return strings.Compare(a.ID().String(), b.ID().String())
		})
		for _, w := range waiting {
			taken := false
			j, err := s.update(ctx, w.ID(), func(x *domain.Job) { taken = x.Begin(fw.Now()) })
			if err != nil {
				return RanDTO{}, err
			}
			if !taken { // cancelled, or another worker has it
				continue
			}
			done, err := s.run(ctx, j)
			if done == nil {
				return RanDTO{}, err
			}
			dto := jobDTO(done)
			return RanDTO{Job: &dto}, err
		}
		return RanDTO{}, nil
	})

	svc.Purge = guard(PermJobRun, func(ctx context.Context, _ Purge) (PurgedDTO, error) {
		out, now := PurgedDTO{Expired: []string{}, GivenUp: []string{}}, fw.Now()
		completed, err := d.Jobs.Find(ctx, domain.JobFieldStatus.Eq(string(domain.Completed)))
		if err != nil {
			return out, err
		}
		for _, j := range completed {
			if !j.Due(now) {
				continue
			}
			key := ""
			if _, err := s.update(ctx, j.ID(), func(x *domain.Job) { key, _ = x.Expire(now) }); err != nil {
				return out, err
			}
			if key != "" {
				_ = d.Files.Remove(ctx, key)
				out.Expired = append(out.Expired, j.ID().String())
			}
		}
		writing, err := d.Jobs.Find(ctx, domain.JobFieldStatus.Eq(string(domain.Processing)))
		if err != nil {
			return out, err
		}
		for _, j := range writing {
			if j.State().StartedAt.After(now.Add(-d.Stuck)) {
				continue
			}
			given := false
			if _, err := s.update(ctx, j.ID(), func(x *domain.Job) { given = x.Fail("the export took too long and was given up", now) }); err != nil {
				return out, err
			}
			if given {
				_ = d.Files.Remove(ctx, j.ID().String()+"."+j.State().Format)
				out.GivenUp = append(out.GivenUp, j.ID().String())
			}
		}
		slices.Sort(out.Expired)
		slices.Sort(out.GivenUp)
		return out, nil
	})

	load := func(ctx context.Context, id domain.JobID) (*domain.Job, error) {
		j, err := d.Jobs.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return j, visible(ctx, j)
	}

	svc.Cancel = guard(PermJobCreate, func(ctx context.Context, c CancelJob) (JobDTO, error) {
		cur, err := load(ctx, c.ID)
		if err != nil {
			return JobDTO{}, err
		}
		if !cur.Active() { // already over: nothing to give up
			return jobDTO(cur), nil
		}
		j, err := s.update(ctx, c.ID, func(x *domain.Job) { x.Cancel(fw.Now()) })
		if err != nil {
			return JobDTO{}, err
		}
		return jobDTO(j), nil
	})

	svc.Get = guard(PermJobRead, func(ctx context.Context, q GetJob) (JobDTO, error) {
		j, err := load(ctx, q.ID)
		if err != nil {
			return JobDTO{}, err
		}
		return jobDTO(j), nil
	})

	svc.Download = guard(PermJobRead, func(ctx context.Context, q DownloadJob) (File, error) {
		j, err := load(ctx, q.ID)
		if err != nil {
			return File{}, err
		}
		st := j.State()
		if st.Status != domain.Completed || j.Due(fw.Now()) {
			return File{}, fw.Violation("exports.no_file", "the export has no file to download: it is "+string(st.Status))
		}
		content, err := d.Files.Open(ctx, st.FileKey)
		if err != nil {
			return File{}, err
		}
		return File{Name: st.FileName, ContentType: domain.ContentType(st.Format), Size: st.Size, Content: content}, nil
	})

	svc.List = guard(PermJobRead, func(ctx context.Context, q ListJobs) (fw.Page[JobDTO], error) {
		ac, ok := authz.FromContext(ctx)
		var filter spec.Specification[*domain.Job] = spec.None[*domain.Job]()
		switch {
		case ok && q.All && ac.GlobalAdmin:
			filter = spec.All[*domain.Job]()
		case ok && q.All:
			return fw.Page[JobDTO]{}, fmt.Errorf("%w: only a global administrator lists the exports of everybody", fw.ErrForbidden)
		case ok:
			filter = domain.JobFieldRequester.Eq(ac.Subject)
		}
		page, err := d.Jobs.FindPage(ctx, filter, fw.NewPageRequest(q.Page, q.Size, domain.JobFieldRequested.Desc()))
		if err != nil {
			return fw.Page[JobDTO]{}, err
		}
		return fw.MapPage(page, jobDTO), nil
	})

	svc.Datasets = guard(PermJobRead, func(ctx context.Context, _ ListDatasets) ([]DatasetDTO, error) {
		out := []DatasetDTO{}
		for _, ds := range d.Registry.all() {
			if authz.Require(ctx, ds.Permission()) != nil {
				continue
			}
			dto := DatasetDTO{Key: ds.Key(), Title: ds.Title(), Columns: []ColumnDTO{}, Filters: append([]string{}, ds.Filters()...)}
			for _, c := range ds.Columns() {
				dto.Columns = append(dto.Columns, ColumnDTO(c))
			}
			out = append(out, dto)
		}
		return out, nil
	})
	return svc
}
