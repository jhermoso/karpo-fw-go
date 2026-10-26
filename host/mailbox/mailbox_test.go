package mailbox_test

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
	"unicode/utf8"

	"github.com/jhermoso/karpo-fw-go/host/mailbox"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	_ "modernc.org/sqlite"
)

// listener es un consumidor que recoge una vez lo que se le da (como asegura el inbox
// de uno real), salvo que se le diga que rechace.
type listener struct {
	name   string
	refuse error
	got    []string
	tried  int
}

func (l *listener) Name() string { return l.name }

func (l *listener) HandleMessage(_ context.Context, env app.Envelope) error {
	l.tried++
	if l.refuse != nil {
		return l.refuse
	}
	if !slices.Contains(l.got, env.ID) {
		l.got = append(l.got, env.ID)
	}
	return nil
}

func message(id, subject string, minute int) app.Envelope {
	return app.Envelope{ID: id, Type: "billing.invoice-issued.v1", Source: "billing", Subject: subject,
		OccurredAt: time.Date(2026, 10, 9, 10, minute, 0, 0, time.UTC), Data: json.RawMessage(`{"n":"` + id + `"}`)}
}

func scenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	office := mailbox.New(sw)
	told := []mailbox.DTO{}
	office.OnGivenUp(func(_ context.Context, d mailbox.DTO) { told = append(told, d) })
	broker := inprocess.NewBroker()
	books := &listener{name: "accounting", refuse: fw.Violation("accounting.no_ledger", "the company has no ledger")}
	dues := &listener{name: "receivables"}
	for _, l := range []*listener{books, dues} {
		broker.Subscribe(l.Name(), office.For(l))
	}
	adminCtx, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "root", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	adminCtx.GlobalAdmin = true
	admin := authz.WithContext(ctx, adminCtx)
	kept := func(status string) []mailbox.DTO {
		t.Helper()
		p, err := office.Search(admin, mailbox.Search{Status: status, Size: 100})
		if err != nil {
			t.Fatal(err)
		}
		return p.Items
	}
	send := func(env app.Envelope) {
		t.Helper()
		if err := broker.Send(ctx, env); err != nil {
			t.Fatalf("the publisher is never told a listener refused: %v", err)
		}
	}

	// Un oyente rechaza un mensaje: el otro lo recibe, al publicador no se le dice, el mensaje se guarda.
	send(message("m1", "inv-1", 1))
	if !slices.Equal(dues.got, []string{"m1"}) || books.tried != 1 {
		t.Fatalf("the other listener: %v, tried %d", dues.got, books.tried)
	}
	w := kept("")
	if len(w) != 1 || w[0].Consumer != "accounting" || w[0].Status != "waiting" || w[0].Attempts != 1 || w[0].Code != "accounting.no_ledger" ||
		w[0].Subject != "inv-1" || w[0].NextAttempt == "" || string(w[0].Data) != `{"n":"m1"}` {
		t.Fatalf("kept: %+v", w)
	}
	// Enviado otra vez: nada nuevo, y al oyente no se le vuelve a preguntar.
	send(message("m1", "inv-1", 1))
	// Sobre lo mismo: espera detrás, sin intentar. Sobre otra cosa: se intenta por su cuenta.
	send(message("m2", "inv-1", 2))
	if books.tried != 1 || len(kept("")) != 2 {
		t.Fatalf("behind: tried %d, kept %+v", books.tried, kept(""))
	}
	books.refuse = nil
	send(message("m3", "inv-2", 3))
	if !slices.Equal(books.got, []string{"m3"}) || !slices.Equal(dues.got, []string{"m1", "m2", "m3"}) {
		t.Fatalf("another thing goes through: %v %v", books.got, dues.got)
	}
	// Aún no toca: una ronda no hace nada. Cuando se pide, van los dos en el orden en que pasaron.
	if r, err := office.Redeliver(ctx, false); err != nil || r != (mailbox.Redelivered{Waiting: 2}) || len(books.got) != 1 {
		t.Fatalf("not due: %+v %v", r, err)
	}
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Delivered: 2}) || !slices.Equal(books.got, []string{"m3", "m1", "m2"}) {
		t.Fatalf("redelivered: %+v %v %v", r, err, books.got)
	}
	if len(kept("")) != 0 {
		t.Fatalf("what was delivered is not kept: %+v", kept(""))
	}

	// Un oyente que sigue rechazando: tras diez intentos el mensaje se abandona, y lo que viene
	// después sobre lo mismo espera.
	books.refuse = errors.New("the database is down")
	send(message("m4", "inv-3", 4))
	send(message("m5", "inv-3", 5))
	for range mailbox.MaxAttempts - 2 {
		if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Waiting: 2}) {
			t.Fatalf("still refusing: %+v %v", r, err)
		}
	}
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Waiting: 1, GivenUp: 1}) {
		t.Fatalf("given up: %+v %v", r, err)
	}
	// Se avisa a alguien, una vez, en el momento en que se abandona.
	if len(told) != 1 || told[0].Consumer != "accounting" || told[0].Status != "given-up" || told[0].Attempts != mailbox.MaxAttempts ||
		told[0].Reason != "the database is down" {
		t.Fatalf("told: %+v", told)
	}
	sum, err := office.Summary(admin)
	if err != nil || len(sum) != 1 || sum[0] != (mailbox.Count{Consumer: "accounting", Waiting: 1, GivenUp: 1}) {
		t.Fatalf("summary: %+v %v", sum, err)
	}
	given := kept("given-up")
	if len(given) != 1 || given[0].Attempts != mailbox.MaxAttempts || given[0].Code != "error" || given[0].Reason != "the database is down" || given[0].NextAttempt != "" {
		t.Fatalf("given up: %+v", given)
	}
	tried := books.tried
	books.refuse = nil
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Waiting: 1, GivenUp: 1}) || books.tried != tried {
		t.Fatalf("what was given up is not tried on its own: %+v %v", r, err)
	}

	// Sólo un administrador global mira los buzones y decide.
	clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	cctx := authz.WithContext(ctx, clerk)
	gid, _ := mailbox.ParseID(given[0].ID)
	if _, err := office.Search(cctx, mailbox.Search{}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("search: %v", err)
	}
	if _, err := office.Summary(cctx); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("summary: %v", err)
	}
	if len(told) != 1 {
		t.Fatalf("told again about the same delivery: %+v", told)
	}
	if _, err := office.Retry(cctx, mailbox.Retry{}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("retry: %v", err)
	}
	if _, err := office.Discard(cctx, mailbox.Discard{ID: gid, Note: "x"}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("discard: %v", err)
	}
	if _, err := office.Search(admin, mailbox.Search{Status: "lost"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("an unknown status: %v", err)
	}
	// Descartar dice por qué; después pasa lo que esperaba detrás.
	var rule *fw.RuleViolationError
	if _, err := office.Discard(admin, mailbox.Discard{ID: gid}); !errors.As(err, &rule) || rule.Code != "mailbox.discard_note" {
		t.Fatalf("discard without a note: %v", err)
	}
	d, err := office.Discard(admin, mailbox.Discard{ID: gid, Note: "posted by hand"})
	if err != nil || d.Status != "discarded" || d.Note != "posted by hand" || d.ResolvedAt == "" {
		t.Fatalf("discard: %+v %v", d, err)
	}
	if _, err := office.Discard(admin, mailbox.Discard{ID: gid, Note: "again"}); !errors.As(err, &rule) || rule.Code != "mailbox.not_open" {
		t.Fatalf("discard twice: %v", err)
	}
	if r, err := office.Redeliver(ctx, false); err != nil || r != (mailbox.Redelivered{Delivered: 1}) || books.got[len(books.got)-1] != "m5" {
		t.Fatalf("what waited behind: %+v %v %v", r, err, books.got)
	}
	send(message("m4", "inv-3", 4)) // el mensaje descartado, enviado otra vez: sigue descartado
	if slices.Contains(books.got, "m4") || len(kept("")) != 1 {
		t.Fatalf("discarded for good: %v %+v", books.got, kept(""))
	}

	// Pedir reintentar devuelve lo abandonado y lo intenta en el acto.
	books.refuse = errors.New("still down")
	send(message("m6", "", 6)) // sobre nada en particular: no retiene a nadie
	send(message("m7", "", 7))
	for range mailbox.MaxAttempts - 1 {
		if _, err := office.Redeliver(ctx, true); err != nil {
			t.Fatal(err)
		}
	}
	if g := kept("given-up"); len(g) != 2 || len(told) != 3 {
		t.Fatalf("two given up: %+v, told %d", g, len(told))
	}
	books.refuse = nil
	one := kept("given-up")[0]
	if r, err := office.Retry(admin, mailbox.Retry{ID: one.ID}); err != nil || r != (mailbox.Redelivered{Delivered: 1, GivenUp: 1}) {
		t.Fatalf("retry one: %+v %v", r, err)
	}
	if r, err := office.Retry(admin, mailbox.Retry{}); err != nil || r != (mailbox.Redelivered{Delivered: 1}) || len(kept("waiting"))+len(kept("given-up")) != 0 {
		t.Fatalf("retry all: %+v %v", r, err)
	}
	if sum, err := office.Summary(admin); err != nil || len(sum) != 0 {
		t.Fatalf("nothing pending: %+v %v", sum, err)
	}
	if _, err := office.Retry(admin, mailbox.Retry{ID: "x"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("retry of nothing: %v", err)
	}
}

func TestMailbox_OnMemory(t *testing.T) {
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	scenario(t, sw)
}

func TestMailbox_OnSQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mailbox.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{mailbox.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scenario(t, hotswap.New(db))
}

// blocking es un oyente que se queda dentro del primer mensaje hasta que la prueba lo suelta, y
// avisa de cada llamada que le llega mientras tanto.
type blocking struct {
	calls   int
	entered chan struct{}
	again   chan struct{}
	release chan struct{}
}

func (*blocking) Name() string { return "accounting" }

func (b *blocking) HandleMessage(context.Context, app.Envelope) error {
	b.calls++
	if b.calls == 1 {
		close(b.entered)
		<-b.release
		return nil
	}
	b.again <- struct{}{}
	return nil
}

// El reintento que pide alguien y la ronda del planificador no recorren a la vez las mismas
// entregas: el oyente recibe el mensaje una vez.
func TestMailbox_RetryWaitsForTheRoundInCourse(t *testing.T) {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	office := mailbox.New(sw)
	refusing := &listener{name: "accounting", refuse: errors.New("down")}
	if err := office.For(refusing).HandleMessage(ctx, message("m1", "inv-1", 1)); err != nil {
		t.Fatal(err)
	}
	l := &blocking{entered: make(chan struct{}), again: make(chan struct{}, 4), release: make(chan struct{})}
	office.For(l)
	root, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "root", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	root.GlobalAdmin = true
	admin := authz.WithContext(ctx, root)

	round, asked := make(chan mailbox.Redelivered, 1), make(chan mailbox.Redelivered, 1)
	go func() { r, _ := office.Redeliver(ctx, true); round <- r }()
	<-l.entered // la ronda está entregando m1
	go func() { r, _ := office.Retry(admin, mailbox.Retry{}); asked <- r }()
	select {
	case <-l.again:
		t.Fatal("the retry delivered the message while the round was delivering it")
	case <-time.After(200 * time.Millisecond):
	}
	close(l.release)
	if r := <-round; r != (mailbox.Redelivered{Delivered: 1}) {
		t.Fatalf("round: %+v", r)
	}
	if r := <-asked; r != (mailbox.Redelivered{}) || l.calls != 1 {
		t.Fatalf("retry: %+v, the listener got %d calls", r, l.calls)
	}
	// Quien se cansa de esperar su turno se va sin tocar nada.
	gone, cancel := context.WithCancel(admin)
	cancel()
	if _, err := office.Retry(gone, mailbox.Retry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled retry: %v", err)
	}
}

// Un mensaje cuyo tipo o identidad no caben en una entrega se guarda una vez, descartado y con su
// motivo, en vez de hacer fallar la validación (y volver al emisor) cada vez.
func TestMailbox_AnEnvelopeThatDoesNotFitIsKeptDiscarded(t *testing.T) {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	office := mailbox.New(sw)
	l := &listener{name: "accounting", refuse: errors.New("down")}
	box := office.For(l)
	root, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "root", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	root.GlobalAdmin = true
	admin := authz.WithContext(ctx, root)

	long := message(strings.Repeat("x", 80), "inv-1", 1)
	long.Type = strings.Repeat("é", 60) // 120 bytes: el recorte no puede partir una «é»
	// Lo descartado no retiene a nadie: el siguiente sobre lo mismo se intenta (y tampoco cabe).
	next := message("m2", "inv-1", 2)
	next.Type = long.Type
	for _, env := range []app.Envelope{long, long, next} {
		if err := box.HandleMessage(ctx, env); err != nil {
			t.Fatalf("the publisher is told it failed: %v", err)
		}
	}
	p, err := office.Search(admin, mailbox.Search{Size: 10})
	if err != nil || len(p.Items) != 2 {
		t.Fatalf("kept: %+v %v", p.Items, err)
	}
	for _, d := range p.Items {
		if d.Status != "discarded" || d.Code != mailbox.OversizedCode || !utf8.ValidString(d.EventType) || len(d.EventType) > 100 || len(d.EventType) < 99 ||
			!strings.Contains(d.Note, "120 bytes") || d.ResolvedAt == "" {
			t.Fatalf("discarded with its reason: %+v", d)
		}
	}
	if k := p.Items[0]; k.EnvelopeID != fw.EnvelopeKey(long.ID) || len(k.EnvelopeID) != 64 || k.Reason != "down" {
		t.Fatalf("the identity that does not fit is kept as its digest: %+v", k)
	}
	if l.tried != 2 {
		t.Fatalf("sent again, it is known: tried %d", l.tried)
	}
	l.refuse = nil
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{}) || l.tried != 2 {
		t.Fatalf("what was discarded is not tried: %+v %v", r, err)
	}
	if fw.EnvelopeKey("m1") != "m1" || fw.EnvelopeKey(long.ID) == fw.EnvelopeKey(long.ID+"y") || fw.EnvelopeKey("") == fw.EnvelopeKey("") {
		t.Fatal("EnvelopeKey")
	}
}

// Dos mensajes distintos sin identidad no se toman por el mismo: el oyente recibe los dos, y se
// guardan los dos.
func TestMailbox_TwoMessagesWithoutIdentityAreBothTried(t *testing.T) {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	office := mailbox.New(sw)
	l := &listener{name: "accounting", refuse: errors.New("down")}
	box := office.For(l)
	root, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "root", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	root.GlobalAdmin = true
	for _, env := range []app.Envelope{message("", "inv-1", 1), message("", "inv-2", 2)} {
		if err := box.HandleMessage(ctx, env); err != nil {
			t.Fatal(err)
		}
	}
	p, err := office.Search(authz.WithContext(ctx, root), mailbox.Search{Size: 10})
	if err != nil || len(p.Items) != 2 || l.tried != 2 || p.Items[0].EnvelopeID == p.Items[1].EnvelopeID || p.Items[0].Code != mailbox.OversizedCode {
		t.Fatalf("kept: %+v %v, tried %d", p.Items, err, l.tried)
	}
}

func TestBackoff(t *testing.T) {
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	d, err := mailbox.Keep(mailbox.NewID(), mailbox.State{Consumer: "c", EventType: "t", EnvelopeID: "e", Data: "{}"}, "x", "y", now)
	if err != nil {
		t.Fatal(err)
	}
	waits := []time.Duration{d.State().NextAttempt.Sub(now)}
	for range 7 {
		d.Refused("x", "y", now)
		waits = append(waits, d.State().NextAttempt.Sub(now))
	}
	if want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}; !slices.Equal(waits, want) {
		t.Fatalf("waits: %v", waits)
	}
}
