// Package domain is the model of the Imports (Importación de datos) bounded context: what a
// source of data yields (records), who writes them (loaders of the host), the run that tells what
// happened and the references that remember which entity each foreign key became.
package domain

import (
	"context"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Aggregate type names.
const (
	RunKind       = "imports.run"
	ReferenceKind = "imports.reference"
)

// Identities of the context.
type (
	// RunID identifies a run.
	RunID struct{ fw.UUID }
	// ReferenceID identifies a reference.
	ReferenceID struct{ fw.UUID }
)

// NewRunID returns a fresh identity.
func NewRunID() RunID { return RunID{fw.NewUUID()} }

// NewReferenceID returns a fresh identity.
func NewReferenceID() ReferenceID { return ReferenceID{fw.NewUUID()} }

// ParseRunID parses a textual identity.
func ParseRunID(s string) (RunID, error) {
	u, err := fw.ParseUUID(s)
	return RunID{u}, err
}

// Limits of what a run takes.
const (
	MaxFileBytes = 5 << 20 // all the files of a run together
	MaxRecords   = 100000
	MaxMessages  = 5000 // kept per run (the C# limit); the rest are counted
)

// GlobalScope is the scope of a key that is unique in its source without a qualifier.
const GlobalScope = "global"

// File is a file of a run. Role says which of the files the source expects it is.
type File struct {
	Role    string
	Name    string
	Content string
}

// Record is one thing a source wants to exist: its kind (legal-entity, person...), the key the
// source knows it by (within a scope) and its fields, all text.
type Record struct {
	Kind   string
	Scope  string
	Key    string
	File   string
	Line   int
	Fields map[string]string
}

// Severities of a message.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Message is something a run has to say about a line of a file or a record.
type Message struct {
	No       int
	Severity string
	Code     string
	Text     string
	File     string
	Line     int
	Kind     string
	Key      string
	Entity   string
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// FileSpec is a file a source expects.
type FileSpec struct {
	Role     string
	Required bool
	About    string
}

// Source turns the files of a system into records. It is pure: it reads no database and decides
// nothing about what already exists.
type Source interface {
	// Key names the source (personio, sage...).
	Key() string
	// Files lists the files it takes.
	Files() []FileSpec
	// Kinds lists the kinds of record it yields, in the order they must be applied.
	Kinds() []string
	// Read parses the files. Lines it cannot use come back as messages; an error means the files
	// cannot be read at all.
	Read(files []File) ([]Record, []Message, error)
}

// Refs answers which entity a key of the running source already is, including what the run has
// created so far.
type Refs interface {
	Lookup(kind, scope, key string) (entityID string, ok bool)
}

// Outcome is what applying a record did.
type Outcome string

// Outcomes.
const (
	Created   Outcome = "created"
	Updated   Outcome = "updated"
	Unchanged Outcome = "unchanged"
)

// Loader writes the records of one kind into the context that owns them. The host implements it
// with the use cases of that context, so its permissions and rules apply to an import as to any
// other caller.
type Loader interface {
	// Kind is the kind of record it takes.
	Kind() string
	// EntityType names what the records become (parties.party...).
	EntityType() string
	// Find looks for the entity by its natural key when the source has not been linked to it yet.
	// It returns "" when there is none and it changes nothing.
	Find(ctx context.Context, r Record, refs Refs) (string, error)
	// Apply creates the entity (existing is "") or brings it up to date, and returns its identity.
	Apply(ctx context.Context, r Record, existing string, refs Refs) (string, Outcome, error)
}

// Count is what happened to the records of a kind. In a preview Created is what would be created
// and Unchanged what already exists.
type Count struct {
	Kind      string
	Read      int
	Created   int
	Updated   int
	Unchanged int
	Skipped   int // no loader takes the kind
	Failed    int
}

// Report gathers the counts and messages of a run while it happens.
type Report struct {
	Counts   []Count
	Messages []Message
	Errors   int
	Warnings int
	Dropped  int
}

// Of returns the count of a kind, adding it when it is new.
func (r *Report) Of(kind string) *Count {
	for i := range r.Counts {
		if r.Counts[i].Kind == kind {
			return &r.Counts[i]
		}
	}
	r.Counts = append(r.Counts, Count{Kind: kind})
	return &r.Counts[len(r.Counts)-1]
}

// Add records a message; beyond MaxMessages it is only counted.
func (r *Report) Add(m Message) {
	if m.Severity == SeverityError {
		r.Errors++
	} else {
		m.Severity = SeverityWarning
		r.Warnings++
	}
	if len(r.Messages) >= MaxMessages {
		r.Dropped++
		return
	}
	m.No = len(r.Messages) + 1
	m.Code, m.Text, m.File, m.Kind, m.Key, m.Entity = clip(m.Code, 100), clip(m.Text, 1000), clip(m.File, 200), clip(m.Kind, 64), clip(m.Key, 256), clip(m.Entity, 128)
	r.Messages = append(r.Messages, m)
}

// Fail records an error about a record.
func (r *Report) Fail(rec Record, code, text string) {
	r.Add(Message{Severity: SeverityError, Code: code, Text: text, File: rec.File, Line: rec.Line, Kind: rec.Kind, Key: rec.Key})
}
