// Package domain es el modelo del contexto delimitado Exports (Exportación de listados): el trabajo
// que convierte en fichero una lista que alguien puede leer, y cómo se escribe el fichero. El C#
// guardaba sus trabajos en memoria y sus ficheros en la carpeta temporal de una máquina.
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

// JobKind es el nombre del tipo de agregado de un trabajo.
const JobKind = "exports.job"

// Límites de un trabajo.
const (
	MaxRows       = 1000000 // una hoja admite 1.048.576 filas
	MaxFilters    = 30
	MaxActiveJobs = 5 // esperando o en marcha, por persona
)

// JobID identifica un trabajo.
type JobID struct{ fw.UUID }

// NewJobID devuelve una identidad nueva.
func NewJobID() JobID { return JobID{fw.NewUUID()} }

// ParseJobID interpreta una identidad en texto.
func ParseJobID(s string) (JobID, error) { u, err := fw.ParseUUID(s); return JobID{u}, err }

// Formatos en que se escribe una lista.
const (
	CSV  = "csv"
	XLSX = "xlsx"
)

// Formats enumera los formatos válidos.
var Formats = []string{CSV, XLSX}

// Status es en qué punto está un trabajo.
type Status string

// Estados. El C# no tenía «cancelado» (era un fallo con un texto) y olvidaba sus trabajos al
// reiniciarse.
const (
	Queued     Status = "queued"
	Processing Status = "processing"
	Completed  Status = "completed"
	Failed     Status = "failed"
	Cancelled  Status = "cancelled"
	Expired    Status = "expired" // su fichero se quitó
)

// Statuses enumera los estados válidos.
var Statuses = []Status{Queued, Processing, Completed, Failed, Cancelled, Expired}

// Access es una organización que el solicitante podía leer al pedir.
type Access struct {
	Organization fw.UUID
	Level        string
}

// Requester es quién pidió la exportación y qué podía ver entonces: el fichero se escribe con esa
// vista, lo ejecute quien lo ejecute y cuando sea.
type Requester struct {
	Subject fw.UUID
	Name    string
	Kind    string
	Party   fw.UUID
	Global  bool
	Scope   []Access
}

// Filter es una condición de la lista, tal como la entiende el dataset.
type Filter struct {
	Name  string
	Value string
}

// JobState es el estado persistido de un trabajo.
type JobState struct {
	Dataset     string
	Format      string
	Title       string
	Filters     []Filter
	Requester   Requester
	Status      Status
	Rows        int
	FileName    string
	FileKey     string // dónde lo guarda el almacenamiento
	Size        int64
	Error       string
	RequestedAt time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	ExpiresAt   time.Time // hasta cuándo se guarda el fichero
	Audit       traits.AuditStamp
}

// Job es una exportación.
type Job struct {
	fw.BaseAggregateRoot[JobID]
	traits.Audited
	s JobState
}

// ValidKey dice si s puede nombrar un dataset: minúsculas, dígitos y guiones.
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

// ReconstituteJob reconstruye un trabajo.
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

// Request pone en cola una exportación.
func Request(id JobID, dataset, format, title string, filters []Filter, by Requester, now time.Time) (*Job, error) {
	fs := slices.Clone(filters)
	slices.SortFunc(fs, func(a, b Filter) int { return strings.Compare(a.Name, b.Name) })
	return ReconstituteJob(id, JobState{Dataset: dataset, Format: strings.ToLower(strings.TrimSpace(format)), Title: title, Filters: fs, Requester: by,
		Status: Queued, RequestedAt: now.UTC()})
}

// State devuelve el estado (los slices son copias).
func (j *Job) State() JobState {
	s := j.s
	s.Filters, s.Requester.Scope = slices.Clone(s.Filters), slices.Clone(s.Requester.Scope)
	return s
}

// Active dice si el trabajo espera o se ejecuta.
func (j *Job) Active() bool { return j.s.Status == Queued || j.s.Status == Processing }

// OwnedBy dice si el sujeto lo pidió.
func (j *Job) OwnedBy(subject fw.UUID) bool {
	return !subject.IsZero() && j.s.Requester.Subject == subject
}

// Begin toma un trabajo que espera para escribirlo. Dice si lo hizo: un trabajo que alguien canceló
// o que tomó otro worker se deja en paz.
func (j *Job) Begin(now time.Time) bool {
	if j.s.Status != Queued {
		return false
	}
	j.s.Status, j.s.StartedAt = Processing, now.UTC()
	return true
}

// Progress anota cuántas filas van escritas.
func (j *Job) Progress(rows int) {
	if j.s.Status == Processing && rows > j.s.Rows {
		j.s.Rows = rows
	}
}

// Complete registra el fichero de un trabajo que se estaba escribiendo y hasta cuándo se guarda. Un
// trabajo cancelado entretanto sigue cancelado: devuelve false y el fichero hay que quitarlo.
func (j *Job) Complete(rows int, fileName, fileKey string, size int64, now time.Time, keep time.Duration) bool {
	if j.s.Status != Processing || fileKey == "" {
		return false
	}
	j.s.Status, j.s.Rows, j.s.FileName, j.s.FileKey, j.s.Size = Completed, rows, fileName, fileKey, size
	j.s.FinishedAt, j.s.ExpiresAt = now.UTC(), now.UTC().Add(keep)
	return true
}

// Fail cierra un trabajo que no se pudo escribir. Lo que se cuenta es para la persona que pidió, no
// el texto de un error interno.
func (j *Job) Fail(reason string, now time.Time) bool {
	if !j.Active() {
		return false
	}
	j.s.Status, j.s.Error, j.s.FinishedAt = Failed, clip(reason, 300), now.UTC()
	return true
}

// Cancel abandona un trabajo que espera o se ejecuta. Dice si cambió algo.
func (j *Job) Cancel(now time.Time) bool {
	if !j.Active() {
		return false
	}
	j.s.Status, j.s.FinishedAt = Cancelled, now.UTC()
	return true
}

// Due dice si el fichero de un trabajo terminado se ha guardado el tiempo suficiente.
func (j *Job) Due(now time.Time) bool { return j.s.Status == Completed && !now.Before(j.s.ExpiresAt) }

// Expire olvida el fichero de un trabajo terminado y devuelve la clave que tenía.
func (j *Job) Expire(now time.Time) (string, bool) {
	if !j.Due(now) {
		return "", false
	}
	key := j.s.FileKey
	j.s.Status, j.s.FileKey = Expired, ""
	return key, true
}

// AuditSnapshot implementa traits.Snapshotter.
func (j *Job) AuditSnapshot() map[string]any {
	return map[string]any{"dataset": j.s.Dataset, "format": j.s.Format, "status": string(j.s.Status), "rows": j.s.Rows,
		"requester": j.s.Requester.Subject.String()}
}

// Campos del trabajo.
var (
	JobFieldStatus    = spec.Comparable("status", func(j *Job) string { return string(j.s.Status) })
	JobFieldRequester = spec.Comparable("requested_by", func(j *Job) fw.UUID { return j.s.Requester.Subject })
	JobFieldDataset   = spec.Comparable("dataset", func(j *Job) string { return j.s.Dataset })
	JobFieldRequested = spec.OrderedBy("requested_at", func(j *Job) time.Time { return j.s.RequestedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// JobRepository guarda los trabajos.
type JobRepository = fw.Repository[JobID, *Job]
