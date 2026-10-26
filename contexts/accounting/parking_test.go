package accounting_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// parkingScenario recorre una empresa que empieza a vender antes de tener libros: lo que Accounting
// aún no puede contabilizar espera, en orden, y se contabiliza cuando se puede.
func parkingScenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	am := accounting.Compose(sw)
	svc := am.Service
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	actx := authz.WithContext(ctx, admin)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	envelope := func(typ, subject string, data any) app.Envelope {
		clock = clock.Add(time.Minute)
		raw, _ := json.Marshal(data)
		return app.Envelope{ID: fw.NewUUID().String(), Type: typ, Source: "test", Subject: subject, OccurredAt: clock, Data: raw}
	}
	invoice := func(company, number, date string) app.Envelope {
		id := fw.NewUUID().String()
		return envelope("billing.invoice-issued.v1", id, map[string]any{"invoiceId": id, "number": number, "seller": company, "customer": fw.NewUUID().String(),
			"issueDate": date, "net": "100.00", "total": "121.00", "taxes": []map[string]any{{"taxCode": "G", "amount": "21.00"}}})
	}
	collection := func(company string, inv app.Envelope) app.Envelope {
		return envelope("receivables.collection-allocated.v1", fw.NewUUID().String(), map[string]any{"collectionId": fw.NewUUID().String(), "seller": company,
			"payer": fw.NewUUID().String(), "method": "cash", "invoiceId": inv.Subject, "installment": 1, "amount": "121.00", "on": "2026-03-12"})
	}
	// books abre el plan de cuentas y la contabilidad de una empresa; roles dice qué cuentas tiene su perfil.
	books := func(company string, roles map[string]string) aapp.LedgerDTO {
		t.Helper()
		for code, name := range map[string]string{"4300": "Clientes", "7000": "Ventas", "4770": "HP IVA repercutido", "5700": "Caja"} {
			_, err := svc.CreateAccount.Handle(actx, aapp.CreateAccount{Company: company, Code: code, Name: name, Postable: true})
			must(err)
		}
		l, err := svc.OpenLedger.Handle(actx, aapp.OpenLedger{Company: company, StartMonth: 1, Accounts: roles})
		must(err)
		return l
	}
	entries := func(company string) int64 {
		t.Helper()
		p, err := svc.SearchEntries.Handle(actx, aapp.SearchEntries{Company: company})
		must(err)
		return p.Total
	}
	parked := func(q aapp.SearchParked) []aapp.ParkedDTO {
		t.Helper()
		q.Size = 50
		p, err := svc.SearchParked.Handle(actx, q)
		must(err)
		return p.Items
	}
	retry := func(company string, posted, waiting int) {
		t.Helper()
		r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{Company: company})
		if err != nil || r.Posted != posted || r.Waiting != waiting {
			t.Fatalf("retry: %+v %v, want %d posted and %d waiting", r, err, posted, waiting)
		}
	}
	violates := func(err error, code string) {
		t.Helper()
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != code {
			t.Fatalf("want %s: %v", code, err)
		}
	}
	shop, old := fw.NewUUID().String(), fw.NewUUID().String()
	full := map[string]string{"customers": "4300", "revenue": "7000", "output-tax": "4770", "cash": "5700"}
	books(old, full)

	// Sólo el consumidor rechaza el hecho de una empresa sin libros: su publicador lo enviaría una y
	// otra vez, y con él retendría a todos los demás oyentes.
	first := invoice(shop, "F-1", "2026-03-10")
	violates(am.Consumer.HandleMessage(ctx, first), "accounting.no_ledger")

	// Tras el aparcamiento, el hecho se acepta y se guarda. Enviado otra vez, no se guarda dos veces.
	must(am.Parking.HandleMessage(ctx, first))
	must(am.Parking.HandleMessage(ctx, first))
	paid := collection(shop, first)
	must(am.Parking.HandleMessage(ctx, paid))
	// Un hecho que no nombra empresa sigue a aquél del que trata.
	cancel := envelope("payroll.payslip-cancelled.v1", first.Subject, map[string]any{"payslipId": first.Subject})
	must(am.Parking.HandleMessage(ctx, cancel))
	// Lo que Accounting no contabiliza no es asunto suyo.
	must(am.Parking.HandleMessage(ctx, envelope("parties.party-registered.v1", first.Subject, map[string]any{"partyId": first.Subject})))
	waiting := parked(aapp.SearchParked{Company: shop})
	if len(waiting) != 3 || waiting[0].Code != "accounting.no_ledger" || waiting[0].Attempts != 1 || waiting[0].EventType != "billing.invoice-issued.v1" ||
		waiting[1].Code != domain.WaitingCode || waiting[2].Code != domain.WaitingCode || waiting[2].Company != shop || waiting[0].Status != "parked" ||
		string(waiting[0].Data) != string(first.Data) {
		t.Fatalf("kept: %+v", waiting)
	}
	if entries(shop) != 0 {
		t.Fatal("nothing posted yet")
	}

	// Una empresa con libros no queda retenida por una sin ellos.
	must(am.Parking.HandleMessage(ctx, invoice(old, "F-9", "2026-03-11")))
	if entries(old) != 1 || len(parked(aapp.SearchParked{Company: old})) != 0 {
		t.Fatal("the company with books posts at once")
	}

	// Volver a intentarlo no contabiliza nada mientras siga el motivo: sólo prueba el primero, que
	// cuenta el intento y espera más; los de detrás no se tocan.
	retry(shop, 0, 3)
	if w := parked(aapp.SearchParked{Company: shop}); w[0].Attempts != 2 || w[1].Attempts != 1 || w[0].Version != 2 || w[0].NextAttempt == "" ||
		w[0].Code != "accounting.no_ledger" {
		t.Fatalf("attempts: %+v", w)
	}

	// La tienda abre sus libros, sin cuenta para la caja: la factura se contabiliza, el cobro se
	// detiene, y lo que vino después se queda detrás.
	ledger := books(shop, map[string]string{"customers": "4300", "revenue": "7000", "output-tax": "4770"})
	retry(shop, 1, 2)
	w := parked(aapp.SearchParked{Company: shop, Status: "parked"})
	if len(w) != 2 || w[0].Code != "accounting.role_undefined" || w[0].EventType != "receivables.collection-allocated.v1" || entries(shop) != 1 {
		t.Fatalf("after the books: %+v, %d entries", w, entries(shop))
	}
	// Mientras algo espera, lo que llega ahora espera también: los hechos se contabilizan en el orden en que ocurrieron.
	late := invoice(shop, "F-2", "2026-03-13")
	must(am.Parking.HandleMessage(ctx, late))
	if entries(shop) != 1 {
		t.Fatal("a new fact does not overtake what waits")
	}
	lid, _ := fw.ParseUUID(ledger.ID)
	_, err := svc.SetProfile.Handle(actx, aapp.SetProfile{ID: domain.LedgerID{UUID: lid}, Accounts: full})
	must(err)
	retry("", 3, 0) // todas las empresas a la vez, como hace el planificador
	if entries(shop) != 3 || len(parked(aapp.SearchParked{Company: shop, Status: "parked"})) != 0 || len(parked(aapp.SearchParked{Company: shop, Status: "posted"})) != 4 {
		t.Fatalf("all posted: %d entries, %+v", entries(shop), parked(aapp.SearchParked{Company: shop}))
	}
	// Ya no espera nada: un hecho se contabiliza al llegar, y uno ya contabilizado no se contabiliza otra vez.
	must(am.Parking.HandleMessage(ctx, invoice(shop, "F-3", "2026-03-14")))
	must(am.Parking.HandleMessage(ctx, first))
	must(am.Consumer.HandleMessage(ctx, first))
	if entries(shop) != 4 {
		t.Fatalf("entries: %d", entries(shop))
	}

	// Un hecho que nunca se contabilizará (está mal) retiene a su empresa hasta que alguien renuncia a él.
	other := fw.NewUUID().String()
	books(other, full)
	wrong := invoice(other, "F-X", "not-a-date")
	good := invoice(other, "F-4", "2026-03-15")
	must(am.Parking.HandleMessage(ctx, wrong))
	must(am.Parking.HandleMessage(ctx, good))
	retry(other, 0, 2)
	stuck := parked(aapp.SearchParked{Company: other})
	if len(stuck) != 2 || stuck[0].Code != "accounting.invalid_event" {
		t.Fatalf("stuck: %+v", stuck)
	}
	wid, _ := domain.ParseParkedID(stuck[0].ID)
	_, err = svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: wid})
	violates(err, "accounting.discard_note")
	gone, err := svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: wid, Note: "Factura de prueba con la fecha mal"})
	if err != nil || gone.Status != "discarded" || gone.Note == "" || gone.ResolvedAt == "" {
		t.Fatalf("discarded: %+v %v", gone, err)
	}
	_, err = svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: wid, Note: "otra vez"})
	violates(err, "accounting.not_parked")
	retry(other, 1, 0)
	if entries(other) != 1 {
		t.Fatalf("entries of the other: %d", entries(other))
	}
	must(am.Parking.HandleMessage(ctx, wrong)) // enviado otra vez después de renunciar a él: sigue descartado
	if len(parked(aapp.SearchParked{Company: other})) != 2 {
		t.Fatal("a discarded fact is not kept again")
	}

	// Cada uno ve lo que espera en sus empresas; nadie reintenta los de otra.
	scoped := func(perms []authz.Permission, orgs ...string) context.Context {
		c := authz.Context{Subject: fw.NewUUID(), SubjectName: "accountant", Kind: authz.Service, Permissions: perms}
		for _, o := range orgs {
			c.Grants = append(c.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
		}
		ac, err := authz.NewContext(c)
		must(err)
		return authz.WithContext(ctx, ac)
	}
	accountant := scoped([]authz.Permission{aapp.PermParkedRead, aapp.PermParkedResolve}, other)
	if p, err := svc.SearchParked.Handle(accountant, aapp.SearchParked{}); err != nil || p.Total != 2 {
		t.Fatalf("the accountant of the other: %+v %v", p, err)
	}
	if _, err := svc.RetryParked.Handle(accountant, aapp.RetryParked{Company: shop}); !errors.Is(err, fw.ErrNotFound) {
		t.Fatalf("another company: %v", err)
	}
	if _, err := svc.SearchParked.Handle(scoped([]authz.Permission{aapp.PermEntryRead}, other), aapp.SearchParked{}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("without the permission: %v", err)
	}
	if _, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Status: "lost"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("status: %v", err)
	}
}

func TestAccounting_KeepsWhatItCannotPostYet_MemoryThenSQLite(t *testing.T) {
	ctx := context.Background()
	mem := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = mem.Close(ctx) })
	parkingScenario(t, mem)

	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{ainfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	parkingScenario(t, hotswap.New(db))
}

// stubInner es un consumidor que responde a cada mensaje lo que dice la prueba.
type stubInner struct {
	answer func(app.Envelope) error
}

func (s *stubInner) Name() string    { return accounting.Source }
func (s *stubInner) Types() []string { return []string{"billing.invoice-issued.v1"} }
func (s *stubInner) HandleMessage(_ context.Context, env app.Envelope) error {
	return s.answer(env)
}

// countedParked cuenta lo que se escribe de los hechos aparcados.
type countedParked struct {
	domain.ParkedRepository
	saves int
}

func (c *countedParked) Save(ctx context.Context, f *domain.ParkedFact) error {
	c.saves++
	return c.ParkedRepository.Save(ctx, f)
}

// parkingOver pone el aparcamiento delante de un consumidor de la prueba, sobre un backend en
// memoria. Devuelve el servicio, el aparcamiento, un contexto que puede hacerlo todo, lo que hace la
// factura de una empresa en un minuto, y el repositorio, que cuenta lo que se escribe.
func parkingOver(t *testing.T, inner aapp.Inner) (*aapp.Service, *aapp.Parking, context.Context, func(company string, minute int) app.Envelope, *countedParked) {
	t.Helper()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	d := aapp.Deps{
		Accounts: hotswap.Repository(sw, ainfra.AccountRepositoryFactory), Ledgers: hotswap.Repository(sw, ainfra.LedgerRepositoryFactory),
		Entries: hotswap.Repository(sw, ainfra.EntryRepositoryFactory), Counters: hotswap.Repository(sw, ainfra.CounterRepositoryFactory),
		UoW: sw,
	}
	parked := &countedParked{ParkedRepository: hotswap.Repository(sw, ainfra.ParkedRepositoryFactory)}
	d.Parked = parked
	d.Parking = aapp.NewParking(inner, d)
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	invoice := func(company string, minute int) app.Envelope {
		id := fw.NewUUID().String()
		raw, _ := json.Marshal(map[string]any{"invoiceId": id, "seller": company})
		return app.Envelope{ID: fw.NewUUID().String(), Type: "billing.invoice-issued.v1", Source: "test", Subject: id,
			OccurredAt: time.Date(2026, 3, 10, 9, minute, 0, 0, time.UTC), Data: raw}
	}
	return aapp.NewService(d), d.Parking, authz.WithContext(context.Background(), admin), invoice, parked
}

// Un hecho que falla por algo que no es una regla detiene su propia empresa y ninguna otra.
func TestParking_AFailureThatIsNoRuleStopsItsCompanyAlone(t *testing.T) {
	inner := &stubInner{answer: func(app.Envelope) error { return fw.Violation("accounting.no_ledger", "the company has no ledger") }}
	svc, parking, actx, invoice, _ := parkingOver(t, inner)
	one, other := fw.NewUUID().String(), fw.NewUUID().String()
	bad, behind, good := invoice(one, 1), invoice(one, 2), invoice(other, 3)
	for _, env := range []app.Envelope{bad, behind, good} {
		if err := parking.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}

	// El hecho más antiguo, el que se intenta primero, falla ahora con una validación; el resto podría contabilizarse.
	tried := []string{}
	inner.answer = func(env app.Envelope) error {
		tried = append(tried, env.ID)
		if env.ID == bad.ID {
			var v fw.Validation
			v.Add("issueDate", "required", "the date of the invoice")
			return v.Err()
		}
		return nil
	}
	// La respuesta lleva lo contabilizado, lo que espera y lo que falló: el reintento acabó.
	r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{})
	if err != nil || r.Posted != 1 || r.Waiting != 2 {
		t.Fatalf("retry: %+v %v, want 1 posted and 2 waiting", r, err)
	}
	if len(r.Failed) != 1 || !strings.Contains(r.Failed[0], bad.ID) || !strings.Contains(r.Failed[0], "issueDate") {
		t.Fatalf("what failed is not given back: %+v", r.Failed)
	}
	if len(tried) != 2 || tried[0] != bad.ID || tried[1] != good.ID {
		t.Fatalf("tried: %v, want the bad one and that of the other company", tried)
	}
	kept := func(company string) []aapp.ParkedDTO {
		t.Helper()
		p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Company: company, Size: 50})
		if err != nil {
			t.Fatal(err)
		}
		return p.Items
	}
	if k := kept(other); len(k) != 1 || k[0].Status != "posted" {
		t.Fatalf("the other company: %+v", k)
	}
	k := kept(one)
	if len(k) != 2 || k[0].Status != "parked" || k[0].Code != domain.FailedCode || !strings.Contains(k[0].Reason, "issueDate") ||
		k[1].Status != "parked" || k[1].Code != domain.WaitingCode {
		t.Fatalf("the company of the fact that failed: %+v", k)
	}
}

// clockAt es un reloj que la prueba adelanta a mano.
type clockAt struct{ now time.Time }

func (c *clockAt) Now() time.Time { return c.now }

func stopTheClock(t *testing.T) *clockAt {
	c := &clockAt{now: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)}
	t.Cleanup(fw.SetClock(c))
	return c
}

// El planificador vuelve cada minuto, pero un hecho que sigue sin poder contabilizarse se intenta, y
// se escribe, cada vez más espaciado: un minuto, dos, cuatro... hasta una hora. Lo que se pide a mano
// se intenta en el acto.
func TestParking_TheSchedulerWaitsLongerEachTime(t *testing.T) {
	clock := stopTheClock(t)
	refusal := fw.Violation("accounting.no_ledger", "the company has no ledger")
	tries := 0
	inner := &stubInner{answer: func(app.Envelope) error { tries++; return refusal }}
	svc, parking, actx, invoice, parked := parkingOver(t, inner)
	company := fw.NewUUID().String()
	if err := parking.HandleMessage(context.Background(), invoice(company, 1)); err != nil {
		t.Fatal(err)
	}
	kept := func() aapp.ParkedDTO {
		t.Helper()
		p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Company: company})
		if err != nil || len(p.Items) != 1 {
			t.Fatalf("kept: %+v %v", p.Items, err)
		}
		return p.Items[0]
	}
	round := func() aapp.RetriedDTO {
		t.Helper()
		r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{Due: true})
		if err != nil || r.Posted != 0 || r.Waiting != 1 {
			t.Fatalf("round: %+v %v", r, err)
		}
		return r
	}
	if f := kept(); f.NextAttempt != "2026-10-26T09:01:00Z" || f.Attempts != 1 {
		t.Fatalf("parked: %+v", f)
	}
	// Antes de que toque, una ronda no lo intenta ni lo escribe.
	for range 25 {
		round()
	}
	if tries != 1 || parked.saves != 1 {
		t.Fatalf("not due: %d tries, %d writes", tries, parked.saves)
	}
	// Un día de rondas, una por minuto: se intenta en los minutos 1, 3, 7, 15, 31 y 63, y desde ahí
	// una vez por hora. Cada intento se escribe una vez.
	for range 24 * 60 {
		clock.now = clock.now.Add(time.Minute)
		round()
	}
	f := kept()
	next, _ := time.Parse(time.RFC3339, f.NextAttempt)
	if tries != 29 || parked.saves != 29 || f.Attempts != 29 || next.Sub(clock.now) > domain.MaxParkedWait || !next.After(clock.now) {
		t.Fatalf("a day of rounds: %d tries, %d writes, %+v; want 29", tries, parked.saves, f)
	}
	// Quien lo pide no espera: se intenta ahora.
	if r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{Company: company}); err != nil || r.Waiting != 1 || tries != 30 {
		t.Fatalf("asked for: %+v %v, %d tries", r, err, tries)
	}
	// Un fallo que no es una regla se devuelve en la ronda que lo intenta, y sólo en ésa: el
	// anfitrión no lo anota cada minuto.
	refusal = errors.New("the database is down")
	reported, before := 0, tries
	for range 3 * 60 {
		clock.now = clock.now.Add(time.Minute)
		if r := round(); len(r.Failed) > 0 {
			reported++
			if !strings.Contains(r.Failed[0], "the database is down") {
				t.Fatalf("failed: %+v", r.Failed)
			}
		}
	}
	if reported != tries-before || reported != 3 {
		t.Fatalf("three hours failing: reported %d times, tried %d", reported, tries-before)
	}
	if f := kept(); f.Code != domain.FailedCode {
		t.Fatalf("the failure is written in the fact: %+v", f)
	}
}

func TestParkedBackoff(t *testing.T) {
	waits := []time.Duration{}
	for n := range 9 {
		waits = append(waits, domain.ParkedBackoff(n+1))
	}
	if want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour,
		time.Hour}; !slices.Equal(waits, want) {
		t.Fatalf("waits: %v", waits)
	}
}

// Los hechos que no nombran empresa no comparten cola: uno que no se puede contabilizar sólo detiene
// lo que viene detrás de él sobre lo mismo.
func TestParking_FactsWithoutCompanyDoNotStopEachOther(t *testing.T) {
	refused := map[string]bool{}
	inner := &stubInner{answer: func(env app.Envelope) error {
		if refused[env.Subject] {
			return fw.Violation("accounting.no_ledger", "the company has no ledger")
		}
		return nil
	}}
	svc, parking, actx, invoice, _ := parkingOver(t, inner)
	stuck, free := invoice("", 1), invoice("", 2)
	behind := invoice("", 3)
	behind.Subject = stuck.Subject
	refused[stuck.Subject], refused[free.Subject] = true, true
	for _, env := range []app.Envelope{stuck, free, behind} {
		if err := parking.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	refused[free.Subject] = false
	r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{})
	if err != nil || r.Posted != 1 || r.Waiting != 2 {
		t.Fatalf("retry: %+v %v, want the free one posted and two waiting", r, err)
	}
	p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Status: "posted"})
	if err != nil || len(p.Items) != 1 || p.Items[0].Subject != free.Subject || p.Items[0].Company != "" {
		t.Fatalf("posted: %+v %v", p.Items, err)
	}
	// Sólo los ve un administrador global: no hay empresa con la que mirar el ámbito de nadie más.
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "accountant", Kind: authz.Service,
		Permissions: []authz.Permission{aapp.PermParkedRead}, Grants: []authz.Grant{{OrganizationID: fw.NewUUID(), Level: authz.Full}}})
	if p, err := svc.SearchParked.Handle(authz.WithContext(context.Background(), ac), aapp.SearchParked{}); err != nil || p.Total != 0 {
		t.Fatalf("an accountant sees them: %+v %v", p, err)
	}
}

// Un hecho cuya identidad no cabe se guarda una vez, descartado y con su motivo, en vez de hacer
// fallar la validación (y volver al emisor) cada vez que llega.
func TestParking_AFactThatDoesNotFitIsKeptDiscarded(t *testing.T) {
	tries := 0
	refusal := fw.Violation("accounting.no_ledger", "the company has no ledger")
	inner := &stubInner{answer: func(app.Envelope) error { tries++; return refusal }}
	svc, parking, actx, invoice, _ := parkingOver(t, inner)
	env := invoice(fw.NewUUID().String(), 1)
	env.ID = strings.Repeat("ñ", 40) // 80 bytes
	for range 2 {
		if err := parking.HandleMessage(context.Background(), env); err != nil {
			t.Fatalf("the publisher is told it failed: %v", err)
		}
	}
	p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{})
	if err != nil || len(p.Items) != 1 || tries != 1 {
		t.Fatalf("kept: %+v %v, %d tries", p.Items, err, tries)
	}
	if k := p.Items[0]; k.Status != "discarded" || k.Code != domain.OversizedCode || !strings.Contains(k.Note, "80 bytes") || k.ResolvedAt == "" ||
		k.NextAttempt != "" {
		t.Fatalf("discarded with its reason: %+v", k)
	}
	if fw.EnvelopeKey(env.ID) == env.ID || len(fw.EnvelopeKey(env.ID)) != 64 || fw.EnvelopeKey("e-1") != "e-1" {
		t.Fatal("EnvelopeKey")
	}
}
