// Package domain is the model of the Exports (Exportación de listados) bounded context: the job
// that turns a list someone may read into a file, and how the file is written. The C# kept its
// jobs in memory and its files in the temporary folder of one machine.
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

// JobKind is the aggregate type name of a job.
const JobKind = "exports.job"

// Limits of a job.
const (
	MaxRows       = 1000000 // a sheet holds 1,048,576 rows
	MaxFilters    = 30
	MaxActiveJobs = 5 // waiting or running, per person
)

// JobID identifies a job.
type JobID struct{ fw.UUID }

// NewJobID returns a fresh identity.
func NewJobID() JobID { return JobID{fw.NewUUID()} }

// ParseJobID parses a textual identity.
func ParseJobID(s string) (JobID, error) { u, err := fw.ParseUUID(s); return JobID{u}, err }

// Formats a list is written in.
const (
	CSV  = "csv"
	XLSX = "xlsx"
)

// Formats lists the valid formats.
var Formats = []string{CSV, XLSX}

// Status is where a job is.
type Status string

// Statuses. The C# had no "cancelled" (it was a failure with a text) and forgot its jobs on a
// restart.
const (
	Queued     Status = "queued"
	Processing Status = "processing"
	Completed  Status = "completed"
	Failed     Status = "failed"
	Cancelled  Status = "cancelled"
	Expired    Status = "expired" // its file was removed
)

// Statuses lists the valid statuses.
var Statuses = []Status{Queued, Processing, Completed, Failed, Cancelled, Expired}

// Access is an organization the requester could read when asking.
type Access struct {
	Organization fw.UUID
	Level        string
}

// Requester is who asked for the export and what they could see then: the file is written with
// that sight, whoever runs the job and whenever.
type Requester struct {
	Subject fw.UUID
	Name    string
	Kind    string
	Party   fw.UUID
	Global  bool
	Scope   []Access
}

// Filter is a condition of the list, as the dataset understands it.
type Filter struct {
	Name  string
	Value string
}

// JobState is the persisted state of a job.
type JobState struct {
	Dataset     string
	Format      string
	Title       string
	Filters     []Filter
	Requester   Requester
	Status      Status
	Rows        int
	FileName    string
	FileKey     string // where the storage keeps it
	Size        int64
	Error       string
	RequestedAt time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	ExpiresAt   time.Time // until when the file is kept
	Audit       traits.AuditStamp
}

// Job is one export.
type Job struct {
	fw.BaseAggregateRoot[JobID]
	traits.Audited
	s JobState
}

// ValidKey reports whether s can name a dataset: lower case letters, digits and dashes.
func ValidKey(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ReconstituteJob rebuilds a job.
func ReconstituteJob(id JobID, s JobState) (*Job, error) {
	base, err := fw.NewBaseAggregateRoot(JobKind, id)
	if err != nil {
		return nil, err
	}
	s.Title = strings.TrimSpace(s.Title)
	var v fw.Validation
	v.Require(ValidKey(s.Dataset), "dataset", "format", "a dataset key")
	v.Require(slices.Contains(Formats, s.Format), "format", "enum", "csv or xlsx")
	v.Require(utf8.RuneCountInString(s.Title) <= 100, "title", "length", "a title of at most 100 characters")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(!s.Requester.Subject.IsZero() && s.Requester.Name != "", "requester", "required", "who asks for it")
	v.Require(!s.RequestedAt.IsZero(), "requestedAt", "required", "when it was asked for")
	v.Require(len(s.Filters) <= MaxFilters, "filter", "count", "too many filters")
	seen := map[string]bool{}
	for _, f := range s.Filters {
		v.Require(f.Name != "" && len(f.Name) <= 64 && !seen[f.Name], "filter", "name", "each filter once, with a name of at most 64 characters")
		v.Require(utf8.RuneCountInString(f.Value) <= 500, "filter", "length", "a filter value of at most 500 characters")
		seen[f.Name] = true
	}
	v.Require((s.Status == Completed) == (s.FileKey != ""), "file", "state", "a completed job, and only it, has a file")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Filters, s.Requester.Scope = slices.Clone(s.Filters), slices.Clone(s.Requester.Scope)
	return &Job{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Request queues an export.
func Request(id JobID, dataset, format, title string, filters []Filter, by Requester, now time.Time) (*Job, error) {
	fs := slices.Clone(filters)
	slices.SortFunc(fs, func(a, b Filter) int { return strings.Compare(a.Name, b.Name) })
	return ReconstituteJob(id, JobState{Dataset: dataset, Format: strings.ToLower(strings.TrimSpace(format)), Title: title, Filters: fs, Requester: by,
		Status: Queued, RequestedAt: now.UTC()})
}

// State returns the state (slices are copies).
func (j *Job) State() JobState {
	s := j.s
	s.Filters, s.Requester.Scope = slices.Clone(s.Filters), slices.Clone(s.Requester.Scope)
	return s
}

// Active reports whether the job waits or runs.
func (j *Job) Active() bool { return j.s.Status == Queued || j.s.Status == Processing }

// OwnedBy reports whether the subject asked for it.
func (j *Job) OwnedBy(subject fw.UUID) bool {
	return !subject.IsZero() && j.s.Requester.Subject == subject
}

// Begin takes a waiting job to write it. It reports whether it did: a job somebody cancelled or
// another worker took is left alone.
func (j *Job) Begin(now time.Time) bool {
	if j.s.Status != Queued {
		return false
	}
	j.s.Status, j.s.StartedAt = Processing, now.UTC()
	return true
}

// Progress notes how many rows are written.
func (j *Job) Progress(rows int) {
	if j.s.Status == Processing && rows > j.s.Rows {
		j.s.Rows = rows
	}
}

// Complete records the file of a job that was being written and until when it is kept. A job
// cancelled meanwhile stays cancelled: it reports false and the file is to be removed.
func (j *Job) Complete(rows int, fileName, fileKey string, size int64, now time.Time, keep time.Duration) bool {
	if j.s.Status != Processing || fileKey == "" {
		return false
	}
	j.s.Status, j.s.Rows, j.s.FileName, j.s.FileKey, j.s.Size = Completed, rows, fileName, fileKey, size
	j.s.FinishedAt, j.s.ExpiresAt = now.UTC(), now.UTC().Add(keep)
	return true
}

// Fail closes a job that could not be written. What is told is for the person who asked, not the
// text of an internal error.
func (j *Job) Fail(reason string, now time.Time) bool {
	if !j.Active() {
		return false
	}
	j.s.Status, j.s.Error, j.s.FinishedAt = Failed, clip(reason, 300), now.UTC()
	return true
}

// Cancel gives up a job that waits or runs. It reports whether it changed anything.
func (j *Job) Cancel(now time.Time) bool {
	if !j.Active() {
		return false
	}
	j.s.Status, j.s.FinishedAt = Cancelled, now.UTC()
	return true
}

// Due reports whether the file of a completed job has been kept long enough.
func (j *Job) Due(now time.Time) bool { return j.s.Status == Completed && !now.Before(j.s.ExpiresAt) }

// Expire forgets the file of a completed job and returns the key it had.
func (j *Job) Expire(now time.Time) (string, bool) {
	if !j.Due(now) {
		return "", false
	}
	key := j.s.FileKey
	j.s.Status, j.s.FileKey = Expired, ""
	return key, true
}

// AuditSnapshot implements traits.Snapshotter.
func (j *Job) AuditSnapshot() map[string]any {
	return map[string]any{"dataset": j.s.Dataset, "format": j.s.Format, "status": string(j.s.Status), "rows": j.s.Rows,
		"requester": j.s.Requester.Subject.String()}
}

// Job fields.
var (
	JobFieldStatus    = spec.Comparable("status", func(j *Job) string { return string(j.s.Status) })
	JobFieldRequester = spec.Comparable("requested_by", func(j *Job) fw.UUID { return j.s.Requester.Subject })
	JobFieldDataset   = spec.Comparable("dataset", func(j *Job) string { return j.s.Dataset })
	JobFieldRequested = spec.OrderedBy("requested_at", func(j *Job) time.Time { return j.s.RequestedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// JobRepository stores jobs.
type JobRepository = fw.Repository[JobID, *Job]
