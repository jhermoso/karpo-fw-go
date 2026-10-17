// Package infrastructure persiste el contexto Exports: mapeo SQL, esquema versionado de los cinco
// motores, factorías intercambiables en caliente y dónde se guardan los ficheros.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	eapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// Context es el nombre del contexto delimitado.
const Context = "exports"

// TableAuditLog es el registro de auditoría del contexto: quién exportó qué.
const TableAuditLog = "exports_audit_log"

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// Las columnas de texto miden cuatro veces los caracteres que guardan: Oracle cuenta bytes.
var schemaDDL = []string{
	`CREATE TABLE exp_jobs (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, dataset_key {str:64} NOT NULL, file_format {str:10} NOT NULL,
	title {str:400}, status {str:20} NOT NULL, row_count {int} NOT NULL, file_name {str:200}, file_key {str:200}, file_size {bigint} NOT NULL,
	error_text {str:1200}, requested_by {uuid} NOT NULL, requested_by_name {str:800} NOT NULL, requester_kind {str:20}, requester_party {uuid},
	requester_global {bool} NOT NULL, requested_at {ts} NOT NULL, started_at {ts}, finished_at {ts}, expires_at {ts}, ` + audit + `)`,
	`CREATE INDEX ix_exp_jobs_status ON exp_jobs (status)`,
	`CREATE INDEX ix_exp_jobs_requester ON exp_jobs (requested_by, status)`,
	`CREATE TABLE exp_job_filters (job_id {uuid} NOT NULL, line_no {int} NOT NULL, filter_name {str:64} NOT NULL, filter_value {str:2000},
	PRIMARY KEY (job_id, line_no), FOREIGN KEY (job_id) REFERENCES exp_jobs (id))`,
	`CREATE TABLE exp_job_scope (job_id {uuid} NOT NULL, line_no {int} NOT NULL, organization {uuid} NOT NULL, access_level {str:20} NOT NULL,
	PRIMARY KEY (job_id, line_no), FOREIGN KEY (job_id) REFERENCES exp_jobs (id))`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return sqlite.AuditDDL(TableAuditLog)
	case "postgres":
		return postgres.AuditDDL(TableAuditLog)
	case "sqlserver":
		return sqlserver.AuditDDL(TableAuditLog)
	case "oracle":
		return oracle.AuditDDL(TableAuditLog)
	case "mysql":
		return mysql.AuditDDL(TableAuditLog)
	}
	return nil
}

// Migrations es el esquema versionado del contexto.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "export jobs", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "audit log", Up: technical},
	}}
}

// Tables enumera las tablas del contexto, las hijas primero (orden de borrado).
var Tables = []string{"exp_job_scope", "exp_job_filters", "exp_jobs", TableAuditLog}

// DropAll quita las tablas del contexto y su historial de migraciones (sólo pruebas).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range Tables {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+Context+"'")
}

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optUUID(u fw.UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
}

func optTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullTime(r *sqlrepo.Row, col string) time.Time {
	if t := r.NullTime(col); t != nil {
		return t.UTC()
	}
	return time.Time{}
}

// Los motores no coinciden en el orden de las hijas: se ordenan aquí.
func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// JobMapping mapea Job a exp_jobs, sus filtros y el alcance de quien pidió.
func JobMapping() sqlrepo.Mapping[domain.JobID, *domain.Job] {
	return sqlrepo.Mapping[domain.JobID, *domain.Job]{
		Table: "exp_jobs",
		Columns: sqlrepo.WithAuditColumns("dataset_key", "file_format", "title", "status", "row_count", "file_name", "file_key", "file_size", "error_text",
			"requested_by", "requested_by_name", "requester_kind", "requester_party", "requester_global", "requested_at", "started_at", "finished_at",
			"expires_at"),
		Dehydrate: func(j *domain.Job) (sqlrepo.Values, error) {
			s := j.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"dataset_key": s.Dataset, "file_format": s.Format, "title": opt(s.Title), "status": string(s.Status),
				"row_count": int64(s.Rows), "file_name": opt(s.FileName), "file_key": opt(s.FileKey), "file_size": s.Size, "error_text": opt(s.Error),
				"requested_by": s.Requester.Subject, "requested_by_name": s.Requester.Name, "requester_kind": opt(s.Requester.Kind),
				"requester_party": optUUID(s.Requester.Party), "requester_global": s.Requester.Global, "requested_at": s.RequestedAt,
				"started_at": optTime(s.StartedAt), "finished_at": optTime(s.FinishedAt), "expires_at": optTime(s.ExpiresAt)}, j.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Job, error) {
			s := domain.JobState{Dataset: r.String("dataset_key"), Format: r.String("file_format"), Title: r.String("title"), Status: domain.Status(r.String("status")),
				Rows: int(r.Int64("row_count")), FileName: r.String("file_name"), FileKey: r.String("file_key"), Size: r.Int64("file_size"),
				Error: r.String("error_text"), RequestedAt: r.Time("requested_at").UTC(), StartedAt: nullTime(r, "started_at"),
				FinishedAt: nullTime(r, "finished_at"), ExpiresAt: nullTime(r, "expires_at"), Audit: r.AuditStamp(),
				Requester: domain.Requester{Subject: r.UUID("requested_by"), Name: r.String("requested_by_name"), Kind: r.String("requester_kind"),
					Party: r.UUID("requester_party"), Global: r.Bool("requester_global")}}
			for _, c := range byNo(children.Of("filters")) {
				s.Filters = append(s.Filters, domain.Filter{Name: c.String("filter_name"), Value: c.String("filter_value")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("scope")) {
				s.Requester.Scope = append(s.Requester.Scope, domain.Access{Organization: c.UUID("organization"), Level: c.String("access_level")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteJob(domain.JobID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Job]{{
			Name: "filters", Table: "exp_job_filters", ForeignKey: "job_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "filter_name", "filter_value"},
			Dehydrate: func(j *domain.Job) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, f := range j.State().Filters {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "filter_name": f.Name, "filter_value": opt(f.Value)})
				}
				return out, nil
			},
		}, {
			Name: "scope", Table: "exp_job_scope", ForeignKey: "job_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "organization", "access_level"},
			Dehydrate: func(j *domain.Job) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, a := range j.State().Requester.Scope {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "organization": a.Organization, "access_level": a.Level})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("exports: unsupported backend %T", b) }

// JobRepositoryFactory construye el repositorio de trabajos.
func JobRepositoryFactory(b hotswap.Backend) (domain.JobRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, JobMapping())
	case *memory.Store:
		return memory.NewRepository[domain.JobID, *domain.Job](db), nil
	}
	return nil, unsupported(b)
}

// AuditLogFactory construye el registro de auditoría.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}

type fileKey string

func (k fileKey) String() string { return string(k) }

// MemoryFiles guarda los ficheros en memoria (pruebas, y un solo proceso que puede perderlos).
type MemoryFiles struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewMemoryFiles devuelve un almacén vacío.
func NewMemoryFiles() *MemoryFiles { return &MemoryFiles{files: map[string][]byte{}} }

type memoryFile struct {
	bytes.Buffer
	store *MemoryFiles
	key   string
}

func (f *memoryFile) Close() error {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	f.store.files[f.key] = slices.Clone(f.Bytes())
	return nil
}

// Create implementa application.Files.
func (m *MemoryFiles) Create(_ context.Context, key string) (io.WriteCloser, error) {
	return &memoryFile{store: m, key: key}, nil
}

// Open implementa application.Files.
func (m *MemoryFiles) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[key]
	if !ok {
		return nil, fw.NotFound("exports.file", fileKey(key))
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// Remove implementa application.Files.
func (m *MemoryFiles) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	return nil
}

// Keys enumera lo que se guarda (pruebas).
func (m *MemoryFiles) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for k := range m.files {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// DiskFiles guarda los ficheros en una carpeta que comparten todas las instancias del anfitrión.
type DiskFiles struct{ dir string }

// NewDiskFiles guarda los ficheros en dir, que crea.
func NewDiskFiles(dir string) (*DiskFiles, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &DiskFiles{dir: dir}, nil
}

// path rechaza una clave que no sea un nombre de fichero simple: las claves se hacen aquí, nunca se
// toman de una petición.
func (d *DiskFiles) path(key string) (string, error) {
	// Las dos barras se rechazan en todos los sistemas: una clave escrita en uno se lee en otro.
	if key == "" || key != filepath.Base(key) || key == "." || key == ".." || strings.ContainsAny(key, `/\`) {
		return "", fmt.Errorf("%w: invalid file key", fw.ErrValidation)
	}
	return filepath.Join(d.dir, key), nil
}

// Create implementa application.Files.
func (d *DiskFiles) Create(_ context.Context, key string) (io.WriteCloser, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
}

// Open implementa application.Files.
func (d *DiskFiles) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Remove implementa application.Files.
func (d *DiskFiles) Remove(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var (
	_ eapp.Files = (*MemoryFiles)(nil)
	_ eapp.Files = (*DiskFiles)(nil)
)
