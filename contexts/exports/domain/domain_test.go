package domain_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

var columns = []domain.Column{{Field: "name", Header: "Nombre", Type: domain.Text}, {Field: "balance", Header: "Saldo", Type: domain.Number},
	{Field: "since", Header: "Alta", Type: domain.Date}, {Field: "active", Header: "Activo", Type: domain.Boolean}, {Field: "notes", Header: "=Notas"}}

func rows() []domain.Row {
	dec, _ := vocab.ParseDecimal("-1234.50")
	day, _ := vocab.ParseDate("2026-10-08")
	return []domain.Row{
		{"name": "Acme, S.A.", "balance": dec, "since": day, "active": true, "notes": "=HYPERLINK(\"http://x\")"},
		{"name": "Dice \"hola\"", "balance": 12, "since": time.Date(2026, 1, 2, 23, 0, 0, 0, time.UTC), "active": false, "notes": "dos\nlíneas"},
		{"name": " Ñandú ", "balance": "n/d", "since": nil, "notes": "-5 unidades", "extra": "not a column"},
		{"name": "@usuario", "balance": "12345678901234567890", "since": time.Time{}, "active": nil, "notes": "+34 600"},
	}
}

func TestCSV(t *testing.T) {
	var out bytes.Buffer
	w, err := domain.NewCSV(&out, columns, domain.Spanish)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows() {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// The mark, CRLF, quotes only where needed, numbers as they are, and nothing a spreadsheet
	// would run: what starts like a formula goes after an apostrophe.
	want := string(rune(0xFEFF)) + "Nombre,Saldo,Alta,Activo,'=Notas\r\n" +
		"\"Acme, S.A.\",-1234.5,2026-10-08,Sí,\"'=HYPERLINK(\"\"http://x\"\")\"\r\n" +
		"\"Dice \"\"hola\"\"\",12,2026-01-02,No,\"dos\nlíneas\"\r\n" +
		"\" Ñandú \",n/d,,,'-5 unidades\r\n" +
		"'@usuario,12345678901234567890,,,'+34 600\r\n"
	if out.String() != want {
		t.Fatalf("csv:\n%q\nwant\n%q", out.String(), want)
	}
}

func TestXLSX(t *testing.T) {
	var out bytes.Buffer
	w, err := domain.NewXLSX(&out, "Clientes: 2026/10 [todos]", columns, domain.Spanish)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows() {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
	}
	if len(parts) != 5 || !strings.Contains(parts["xl/workbook.xml"], `<sheet name="Clientes  2026 10  todos" sheetId="1"`) {
		t.Fatalf("workbook: %v %s", len(parts), parts["xl/workbook.xml"])
	}
	sheet := parts["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`<row r="1"><c r="A1" t="inlineStr"><is><t xml:space="preserve">Nombre</t></is></c>`,
		`<c r="E1" t="inlineStr"><is><t xml:space="preserve">=Notas</t></is></c>`, // a text cell is never a formula
		`<c r="B2"><v>-1234.5</v></c>`, `<c r="B3"><v>12</v></c>`,
		`<c r="C2" t="inlineStr"><is><t xml:space="preserve">2026-10-08</t></is></c>`,
		`<c r="D2" t="inlineStr"><is><t xml:space="preserve">Sí</t></is></c>`,
		`=HYPERLINK(&#34;http://x&#34;)`, "dos&#xA;líneas",
		`<c r="B4" t="inlineStr"><is><t xml:space="preserve">n/d</t></is></c>`,         // not a number: a text
		`<c r="B5" t="inlineStr"><is><t xml:space="preserve">12345678901234567890</t>`, // too long to be kept exact as a number
		`<row r="5"><c r="A5"`, `</sheetData></worksheet>`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("sheet lacks %s:\n%s", want, sheet)
		}
	}
	if strings.Contains(sheet, `r="C4"`) || strings.Contains(sheet, "not a column") {
		t.Fatalf("empty cells are left out:\n%s", sheet)
	}
	if domain.SheetName("") != "Export" || len([]rune(domain.SheetName(strings.Repeat("ñ", 40)))) != 31 {
		t.Fatal("sheet name")
	}
}

func TestJob_Lifecycle(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	by := domain.Requester{Subject: fw.NewUUID(), Name: "clerk", Kind: "human", Scope: []domain.Access{{Organization: fw.NewUUID(), Level: "Full"}}}
	request := func() *domain.Job {
		t.Helper()
		j, err := domain.Request(domain.NewJobID(), "customers", " XLSX ", " Clientes ", []domain.Filter{{Name: "name", Value: "a"}, {Name: "city", Value: "b"}}, by, now)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	j := request()
	if s := j.State(); s.Status != domain.Queued || s.Format != domain.XLSX || s.Title != "Clientes" || s.Filters[0].Name != "city" || !j.Active() ||
		!j.OwnedBy(by.Subject) || j.OwnedBy(fw.NewUUID()) || j.OwnedBy(fw.UUID{}) {
		t.Fatalf("requested: %+v", s)
	}
	for _, bad := range []func() (*domain.Job, error){
		func() (*domain.Job, error) {
			return domain.Request(domain.NewJobID(), "Customers", "csv", "", nil, by, now)
		},
		func() (*domain.Job, error) {
			return domain.Request(domain.NewJobID(), "customers", "pdf", "", nil, by, now)
		},
		func() (*domain.Job, error) {
			return domain.Request(domain.NewJobID(), "customers", "csv", "", nil, domain.Requester{}, now)
		},
		func() (*domain.Job, error) {
			return domain.Request(domain.NewJobID(), "customers", "csv", "", []domain.Filter{{Name: "a"}, {Name: "a"}}, by, now)
		},
	} {
		if _, err := bad(); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("invalid request: %v", err)
		}
	}
	if j.Complete(1, "f", "k", 1, now, time.Hour) {
		t.Fatal("a job that waits has no file")
	}
	if !j.Begin(now) || j.Begin(now) {
		t.Fatal("a job is taken once")
	}
	j.Progress(2000)
	j.Progress(100)
	if j.State().Rows != 2000 {
		t.Fatalf("progress only grows: %d", j.State().Rows)
	}
	if j.Complete(3, "f", "", 1, now, time.Hour) || !j.Complete(2500, "export.xlsx", "k1", 99, now.Add(time.Minute), time.Hour) {
		t.Fatal("complete")
	}
	if s := j.State(); s.Status != domain.Completed || s.Rows != 2500 || s.ExpiresAt != now.Add(61*time.Minute) || j.Cancel(now) || j.Fail("x", now) {
		t.Fatalf("completed: %+v", s)
	}
	if _, err := domain.ReconstituteJob(j.ID(), j.State()); err != nil {
		t.Fatal(err)
	}
	if j.Due(now.Add(60*time.Minute)) || !j.Due(now.Add(61*time.Minute)) {
		t.Fatal("due")
	}
	if key, ok := j.Expire(now.Add(2 * time.Hour)); !ok || key != "k1" || j.State().Status != domain.Expired || j.State().FileKey != "" {
		t.Fatalf("expire: %s %+v", key, j.State())
	}
	if _, ok := j.Expire(now.Add(3 * time.Hour)); ok {
		t.Fatal("expired once")
	}

	// Cancelled while it was being written: it stays cancelled.
	c := request()
	c.Begin(now)
	if !c.Cancel(now) || c.Cancel(now) || c.Complete(1, "f", "k", 1, now, time.Hour) || c.State().Status != domain.Cancelled {
		t.Fatalf("cancelled: %+v", c.State())
	}
	f := request()
	if !f.Fail(strings.Repeat("é", 400), now) || len([]rune(f.State().Error)) != 300 || f.Begin(now) {
		t.Fatalf("failed: %+v", f.State())
	}
}
