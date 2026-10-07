package domain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

const orgUnits = `unitType,name,parentOrg,notes
InternalOrganization,Maccorp Exact Change,,
InternalOrganization,Karpo Servicios,,
Department,Operaciones,Maccorp Exact Change,
Department,Operaciones,Karpo Servicios,homónimo
Office,Oficina Sol,Maccorp Exact Change,
Office,Aeropuerto T4,,
Department,Sin dueño,,
Department,Fantasma,No Existe SL,
Department,OPERACIONES,maccorp exact change,repetido
`

const people = `employeeNumber,firstName,lastName,preferredName,email,gender,legalEntity,department,workCenter,jobTitle,isSupervisor,notes,Fecha de contratación,fechaBaja
P-MC-001,Ana,García,,Ana@Maccorp.test,F,Maccorp Exact Change,Operaciones,Oficina Sol,"Auxiliar, Caja",Sí,,01/03/2020,
P-MC-002,Luis,Pérez,Lucho Pérez,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,Oficina Sol,Cajero,no,,2021-05-10,2024-12-31
P-MC-006,Eva,Ruiz,,eva@maccorp.test,F,Karpo Servicios,Operaciones,,Gerente,1,,,
P-MC-007,Sin,Empresa,,x@y.test,,Otra SA,,,,,,,
P-MC-001,Dup,Licado,,d@y.test,,Karpo Servicios,,,,,,,
,No,Number,,,,Karpo Servicios,,,,,,,
P-MC-008,Mal,Fechas,,m@y.test,,Karpo Servicios,,,,,,31/02/2020,
P-MC-009,Al,Revés,,r@y.test,,Karpo Servicios,,,,,,2024-01-01,2023-01-01
`

func of(records []domain.Record, kind string) []domain.Record {
	out := []domain.Record{}
	for _, r := range records {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func codes(ms []domain.Message, severity string) string {
	out := []string{}
	for _, m := range ms {
		if m.Severity == severity {
			out = append(out, fmt.Sprintf("%s:%d:%s", m.File, m.Line, m.Code))
		}
	}
	return strings.Join(out, " ")
}

func TestPersonio_ReadsUnitsAndPeople(t *testing.T) {
	records, messages, err := domain.Personio{}.Read([]domain.File{
		{Role: domain.RolePeople, Name: "people.csv", Content: people},
		{Role: domain.RoleOrgUnits, Name: "org.csv", Content: string(rune(0xFEFF)) + orgUnits}})
	if err != nil {
		t.Fatal(err)
	}
	legal, deps, centers, persons, jobs := of(records, domain.KindLegalEntity), of(records, domain.KindDepartment), of(records, domain.KindWorkCenter),
		of(records, domain.KindPerson), of(records, domain.KindEmployment)
	if len(legal) != 2 || len(deps) != 2 || len(centers) != 2 || len(persons) != 3 || len(jobs) != 4 {
		t.Fatalf("records: %d %d %d %d %d", len(legal), len(deps), len(centers), len(persons), len(jobs))
	}
	if legal[0].Key != "Maccorp Exact Change" || legal[0].Scope != domain.GlobalScope || legal[0].Line != 2 || legal[0].File != "org.csv" {
		t.Fatalf("legal entity: %+v", legal[0])
	}
	// Two departments of the same name are two, one in each company.
	if deps[0].Scope != "Maccorp Exact Change" || deps[1].Scope != "Karpo Servicios" || deps[0].Key != "Operaciones" || deps[1].Fields["notes"] != "homónimo" {
		t.Fatalf("departments: %+v", deps)
	}
	if centers[1].Key != "Aeropuerto T4" || centers[1].Scope != domain.GlobalScope || centers[1].Fields["legalEntity"] != "" {
		t.Fatalf("a work center of nobody: %+v", centers[1])
	}
	if e := codes(messages, domain.SeverityError); e != "org.csv:8:personio.unknown_legal_entity org.csv:9:personio.unknown_legal_entity "+
		"people.csv:5:personio.unknown_legal_entity people.csv:6:personio.duplicate_person people.csv:7:personio.person_without_number "+
		"people.csv:8:personio.dates people.csv:9:personio.dates" {
		t.Fatalf("errors: %s", e)
	}
	if w := codes(messages, domain.SeverityWarning); w != "org.csv:10:personio.duplicate_unit" {
		t.Fatalf("warnings: %s", w)
	}

	ana := persons[0]
	if ana.Key != "P-MC-001" || ana.Fields["fullName"] != "Ana García" || ana.Fields["email"] != "ana@maccorp.test" || ana.Line != 2 {
		t.Fatalf("ana: %+v", ana)
	}
	if j := jobs[0]; j.Scope != "Maccorp Exact Change" || j.Key != "P-MC-001" || j.Fields["jobTitle"] != "Auxiliar, Caja" || j.Fields["supervisor"] != "true" ||
		j.Fields["hireDate"] != "2020-03-01" || j.Fields["terminationDate"] != "" || j.Fields["primary"] != "true" {
		t.Fatalf("ana's job: %+v", j)
	}
	// Someone employed by two companies is one person with one employment in each; only the first
	// keeps the department, the work center and the job.
	if persons[1].Fields["fullName"] != "Lucho Pérez" {
		t.Fatalf("preferred name: %+v", persons[1])
	}
	a, b := jobs[1], jobs[2]
	if a.Scope != "Maccorp Exact Change" || b.Scope != "Karpo Servicios" || a.Key != "P-MC-002" || b.Key != "P-MC-002" || a.Fields["department"] != "Operaciones" ||
		b.Fields["department"] != "" || b.Fields["jobTitle"] != "" || b.Fields["primary"] != "false" || b.Fields["terminationDate"] != "2024-12-31" {
		t.Fatalf("two employments: %+v %+v", a, b)
	}
}

func TestPersonio_OneCompanyOwnsWhatNamesNoParent(t *testing.T) {
	records, messages, err := domain.Personio{}.Read([]domain.File{{Role: domain.RoleOrgUnits, Name: "org.csv",
		Content: "unitType,name,parentOrg,notes\nDepartment,Ventas,,\nInternalOrganization,Acme,,\nOffice,Central,,\n"}})
	if err != nil || len(messages) != 0 || len(records) != 3 {
		t.Fatalf("%v %+v %+v", err, messages, records)
	}
	if records[0].Kind != domain.KindLegalEntity || records[1].Scope != "Acme" || records[2].Scope != "Acme" {
		t.Fatalf("records: %+v", records)
	}
}

func TestParseTable(t *testing.T) {
	tb, err := domain.ParseTable(domain.File{Name: "a.csv", Content: "Código Empresa;Nombre\n\n1;\"Uno;\ny medio\"\n2;Dos;sobra\n3\n"}, ';')
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Rows) != 3 || tb.Rows[0].Get("codigo_empresa") != "1" || tb.Rows[0].Get("NOMBRE") != "Uno;\ny medio" || tb.Rows[0].Line != 3 ||
		tb.Rows[1].Line != 5 || tb.Rows[2].Get("nombre") != "" || tb.Rows[2].Get("otra", "codigoEmpresa") != "3" || !tb.Has("nombre") || tb.Has("otra") {
		t.Fatalf("table: %+v", tb.Rows)
	}
	var rv *fw.RuleViolationError
	if _, err := domain.ParseTable(domain.File{Name: "b.csv", Content: "a,b\n1,\"x\n"}, ','); !errors.As(err, &rv) || rv.Code != "imports.unreadable_file" {
		t.Fatalf("unreadable: %v", err)
	}
	if _, err := domain.ParseTable(domain.File{Name: "c.csv", Content: ""}, ','); !errors.As(err, &rv) {
		t.Fatalf("empty: %v", err)
	}
	if d, ok := domain.Day("31.12.2024"); !ok || d != "2024-12-31" {
		t.Fatalf("day: %s", d)
	}
	if _, ok := domain.Day("mañana"); ok {
		t.Fatal("not a day")
	}
	if !domain.Truth(" SÍ ") || domain.Truth("no") || domain.Fold("  Pérez   Ñu ") != "perez ñu" {
		t.Fatal("truth and fold")
	}
}

func TestRun_EndsOnceAndKeepsWhatItDid(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	start := func() *domain.Run {
		r, err := domain.StartRun(domain.NewRunID(), "personio", []string{"org.csv", "a|b.csv", " "}, "u1", "Importer", now)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	var rep domain.Report
	rep.Of("person").Read = 2
	rep.Of("person").Created = 1
	rep.Fail(domain.Record{Kind: "person", Key: "P1", File: "p.csv", Line: 3}, "x.y", strings.Repeat("é", 1200))
	rep.Add(domain.Message{Severity: "note", Text: "anything else is a warning"})

	r := start()
	if s := r.State(); s.Status != domain.Running || len(s.Files) != 2 || s.Files[1] != "a_b.csv" {
		t.Fatalf("started: %+v", s)
	}
	if err := r.Finish(rep, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s := r.State()
	if s.Status != domain.CompletedWithErrors || s.Errors != 1 || s.Warnings != 1 || len(s.Messages) != 2 || len([]rune(s.Messages[0].Text)) != 1000 ||
		s.Messages[1].Severity != domain.SeverityWarning || s.Messages[1].No != 2 || len(r.PendingEvents()) != 1 {
		t.Fatalf("finished: %+v", s)
	}
	if err := r.Finish(rep, now); err == nil {
		t.Fatal("a run ends once")
	}
	if r.Abort(rep, "late", now) {
		t.Fatal("a run that ended is not aborted")
	}

	clean := start()
	if err := clean.Finish(domain.Report{}, now); err != nil || clean.State().Status != domain.Succeeded {
		t.Fatalf("clean: %v %+v", err, clean.State())
	}

	// Given up for dead and then finishing: it stays failed, with what it did.
	dead := start()
	if !dead.Abort(domain.Report{}, "stale", now) || dead.State().Status != domain.Failed || dead.State().Reason != "stale" {
		t.Fatalf("aborted: %+v", dead.State())
	}
	if err := dead.Finish(rep, now.Add(time.Hour)); err != nil || dead.State().Status != domain.Failed || len(dead.State().Counts) != 1 ||
		len(dead.PendingEvents()) != 1 {
		t.Fatalf("finishing a dead run: %v %+v", err, dead.State())
	}

	// Beyond the limit, messages are counted and not kept.
	var many domain.Report
	for i := 0; i < domain.MaxMessages+7; i++ {
		many.Fail(domain.Record{Kind: "person"}, "x", "y")
	}
	if len(many.Messages) != domain.MaxMessages || many.Dropped != 7 || many.Errors != domain.MaxMessages+7 {
		t.Fatalf("many: %d %d %d", len(many.Messages), many.Dropped, many.Errors)
	}
}

func TestReference_Link(t *testing.T) {
	run := domain.NewRunID()
	ref, err := domain.Link(domain.NewReferenceID(), "personio", domain.Record{Kind: "person", Key: " P1 "}, "parties.party", "abc", run)
	if err != nil || ref.State().Scope != domain.GlobalScope || ref.State().Key != "P1" || ref.State().LastRun != run {
		t.Fatalf("%v %+v", err, ref)
	}
	if _, err := domain.Link(domain.NewReferenceID(), "Personio", domain.Record{Kind: "person", Key: "P1"}, "parties.party", "abc", run); err == nil {
		t.Fatal("a source key is lower case")
	}
	if _, err := domain.Link(domain.NewReferenceID(), "personio", domain.Record{Kind: "person", Key: "P1"}, "parties.party", "", run); err == nil {
		t.Fatal("a reference points at something")
	}
}
