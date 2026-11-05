//go:build smoke

package smoke_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/host/smoke"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// database es un fichero SQLite nuevo, migrado: sobre lo que arranca cmd/karpo-smoke. Lo que
// guarda un backend en memoria no está para la siguiente composición del anfitrión; lo que guarda
// una base de datos, sí.
func database(t *testing.T) *hotswap.Switch {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "karpo.db"))+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return hotswap.New(db)
}

// backend es un backend nuevo en memoria, con los catálogos de Geography: lo que es una base de
// datos migrada que nadie administra aún.
func backend(t *testing.T) *hotswap.Switch {
	t.Helper()
	store := memory.NewStore("memory")
	if err := geoinfra.LoadMemory(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return sw
}

// noCredentials deja el entorno como lo quiere el publicador: sin las credenciales del primer
// administrador.
func noCredentials(t *testing.T) {
	t.Helper()
	t.Setenv(security.EnvBootstrapUser, "")
	t.Setenv(security.EnvBootstrapPassword, "")
}

func publisher(t *testing.T) *smoke.Publisher {
	t.Helper()
	noCredentials(t)
	p, err := smoke.New(context.Background(), smoke.Development, backend(t), smoke.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// do envía una petición a un handler, como desde esta máquina, y da el estado y el cuerpo.
func do(t *testing.T, to http.Handler, method, path, token string, body any) (int, []byte, http.Header) {
	t.Helper()
	return doAs(t, to, "127.0.0.1:8092", method, path, token, body)
}

// doAs es do con el Host que dice la petición.
func doAs(t *testing.T, to http.Handler, host, method, path, token string, body any) (int, []byte, http.Header) {
	t.Helper()
	var sent bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&sent).Encode(body)
	}
	request := httptest.NewRequest(method, path, &sent)
	request.Host = host
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	to.ServeHTTP(recorder, request)
	raw, _ := io.ReadAll(recorder.Body)
	return recorder.Code, raw, recorder.Header()
}

func session(t *testing.T, raw []byte) smoke.Session {
	t.Helper()
	var s smoke.Session
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return s
}

// La primera barrera del arranque: nada más que KARPO_ENV=development, exactamente.
func TestNew_RefusesOutsideDevelopment(t *testing.T) {
	noCredentials(t)
	for _, env := range []string{"", "production", "Development", "DEVELOPMENT", " development", "development ", "dev", "staging", "test"} {
		p, err := smoke.New(context.Background(), env, backend(t), smoke.Options{})
		if err == nil || p != nil {
			t.Fatalf("KARPO_ENV=%q: the publisher started", env)
		}
		if !strings.Contains(err.Error(), smoke.EnvVar) {
			t.Fatalf("KARPO_ENV=%q: the refusal does not name the variable: %v", env, err)
		}
		if smoke.CheckEnv(env) == nil {
			t.Fatalf("CheckEnv(%q) lets it start", env)
		}
	}
	if err := smoke.CheckEnv("development"); err != nil {
		t.Fatal(err)
	}
}

// La segunda barrera del arranque: sólo esta máquina.
func TestListen_RefusesWhatIsNotThisMachine(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8092", ":8092", "[::]:8092", "192.168.1.10:8092", "10.0.0.1:8092", "example.com:8092", "host.docker.internal:8092", "127.0.0.1", "localhost",
		"localhost:8092", "LOCALHOST:8092", "localhost.:8092"} {
		if err := smoke.CheckAddress(address); err == nil {
			t.Errorf("%q is accepted", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8092", "127.0.0.2:0", "[::1]:8092"} {
		if err := smoke.CheckAddress(address); err != nil {
			t.Errorf("%q is refused: %v", address, err)
		}
	}
	p := publisher(t)
	if l, err := p.Listen("0.0.0.0:0"); err == nil {
		_ = l.Close()
		t.Fatal("the publisher listens on every interface")
	}
	l, err := p.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
}

// No toma credenciales, y no abre una puerta sobre una base de datos que alguien administra.
func TestNew_TakesNoCredentialsAndOnlyANewDatabase(t *testing.T) {
	noCredentials(t)
	sw := database(t)
	t.Setenv(security.EnvBootstrapPassword, "a-password-somebody-knows")
	if _, err := smoke.New(context.Background(), smoke.Development, sw, smoke.Options{}); err == nil || !strings.Contains(err.Error(), security.EnvBootstrapPassword) {
		t.Fatalf("it started with the password of the first administrator in the environment: %v", err)
	}
	t.Setenv(security.EnvBootstrapPassword, "")
	t.Setenv(security.EnvBootstrapUser, "root")
	if _, err := smoke.New(context.Background(), smoke.Development, sw, smoke.Options{}); err == nil || !strings.Contains(err.Error(), security.EnvBootstrapUser) {
		t.Fatalf("it started with the user of the first administrator in the environment: %v", err)
	}
	t.Setenv(security.EnvBootstrapUser, "")
	if _, err := smoke.New(context.Background(), smoke.Development, sw, smoke.Options{}); err != nil {
		t.Fatal(err)
	}
	// La misma base de datos otra vez: ahora tiene un administrador, y nadie sabe su contraseña.
	if _, err := smoke.New(context.Background(), smoke.Development, sw, smoke.Options{}); err == nil || !strings.Contains(err.Error(), "new database") {
		t.Fatalf("it started on a database that already has an administrator: %v", err)
	}
}

// Una petición que no nombra esta máquina en su Host no recibe nada, ni siquiera del anfitrión
// tras la puerta: eso es lo que envía una página de cualquier sitio cuando se hace que su nombre
// se resuelva aquí.
func TestHost_OnlyThisMachine(t *testing.T) {
	p := publisher(t)
	_, raw, _ := do(t, p, "GET", "/api/smoke/token", "", nil)
	token := session(t, raw).AccessToken
	for _, host := range []string{"", "evil.example", "evil.example:8092", "127.0.0.1.evil.example:8092", "localhost.evil.example", "192.168.1.10:8092",
		"0.0.0.0:8092", "[::]:8092", "host.docker.internal:8092"} {
		for _, path := range []string{"/api/smoke/token", "/api/smoke/enter?as=admin", "/api/parties", "/healthz"} {
			if status, body, _ := doAs(t, p, host, "GET", path, token, nil); status != http.StatusForbidden || strings.Contains(string(body), "accessToken") {
				t.Errorf("Host %q, %s: %d, want 403", host, path, status)
			}
		}
		if status, _, _ := doAs(t, p, host, "POST", "/api/auth/login", "", map[string]any{"username": "admin", "password": "x"}); status != http.StatusForbidden {
			t.Errorf("Host %q, login: %d, want 403", host, status)
		}
	}
	// Cualquier puerto: el del publicador, o el que envía el proxy de desarrollo del front.
	for _, host := range []string{"127.0.0.1", "127.0.0.1:8092", "[::1]", "[::1]:8092", "localhost", "localhost:8092", "localhost:4200", "LocalHost:8081"} {
		if status, body, _ := doAs(t, p, host, "GET", "/api/smoke/token", "", nil); status != http.StatusOK {
			t.Errorf("Host %q: %d %s, want 200", host, status, body)
		}
		if status, _, _ := doAs(t, p, host, "GET", "/api/smoke/enter?as=admin", "", nil); status != http.StatusOK {
			t.Errorf("Host %q, the page of entry: %d, want 200", host, status)
		}
	}
}

// La puerta: una sesión sin credenciales, de un administrador que no tiene que cambiar antes una
// contraseña, y nada para quien no trae sesión.
func TestDoor_LetsTheProfileIn(t *testing.T) {
	p := publisher(t)

	if status, _, _ := do(t, p, "GET", "/api/parties", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("without a session: %d, want 401", status)
	}
	if status, _, _ := do(t, p, "GET", "/api/parties", "not-a-token", nil); status != http.StatusUnauthorized {
		t.Fatalf("with a bad token: %d, want 401", status)
	}

	for _, as := range []string{"", "?as=admin", "?as=smoke-admin"} {
		status, raw, header := do(t, p, "GET", "/api/smoke/token"+as, "", nil)
		if status != http.StatusOK || header.Get("Cache-Control") != "no-store" {
			t.Fatalf("token%s: %d %s", as, status, raw)
		}
		s := session(t, raw)
		if !s.Success || s.AccessToken == "" || s.RefreshToken == "" || s.Username != "smoke-admin" || s.Profile != "admin" || s.MustChangePassword || s.UserID == "" {
			t.Fatalf("token%s: %+v", as, s)
		}
	}
	if status, _, _ := do(t, p, "GET", "/api/smoke/token?as=root", "", nil); status != http.StatusBadRequest {
		t.Fatalf("a profile there is not: %d, want 400", status)
	}

	_, raw, _ := do(t, p, "GET", "/api/smoke/token?as=admin", "", nil)
	s := session(t, raw)
	// El anfitrión sabe quién es, y que puede hacerlo todo.
	status, raw, _ := do(t, p, "GET", "/api/auth/context", s.AccessToken, nil)
	var who authz.Resolution
	if err := json.Unmarshal(raw, &who); status != http.StatusOK || err != nil || who.Context == nil {
		t.Fatalf("context: %d %s", status, raw)
	}
	if !who.Context.GlobalAdmin || who.Context.SubjectName != "smoke-admin" || who.Context.Kind != authz.Human {
		t.Fatalf("who gets in: %+v", who.Context)
	}
	// No es 403: la contraseña ya se cambió, nadie tiene que hacerlo.
	if status, raw, _ := do(t, p, "GET", "/api/parties", s.AccessToken, nil); status != http.StatusOK {
		t.Fatalf("a route of a context: %d %s", status, raw)
	}
	if status, raw, _ := do(t, p, "POST", "/api/organizations", s.AccessToken, map[string]any{"legalName": "Humo S.L."}); status != http.StatusCreated {
		t.Fatalf("it may write: %d %s", status, raw)
	}
	// La sesión es del anfitrión: el anfitrión la refresca.
	status, raw, _ = do(t, p, "POST", "/api/auth/refresh", "", map[string]any{"refreshToken": s.RefreshToken})
	if again := session(t, raw); status != http.StatusOK || again.AccessToken == "" || again.RefreshToken == s.RefreshToken {
		t.Fatalf("refresh: %d %s", status, raw)
	}
	// Y las sondas del programa real están ahí.
	if status, _, _ := do(t, p, "GET", "/healthz", "", nil); status != http.StatusOK {
		t.Fatalf("healthz: %d", status)
	}
}

// El login de los perfiles no mira ninguna contraseña; el de cualquier otro es el del anfitrión.
func TestLogin_AnyPasswordForTheProfilesOnly(t *testing.T) {
	p := publisher(t)
	for _, username := range []string{"admin", "smoke-admin"} {
		for _, password := range []string{"", "x", "whatever-it-is"} {
			status, raw, _ := do(t, p, "POST", "/api/auth/login", "", map[string]any{"username": username, "password": password})
			if s := session(t, raw); status != http.StatusOK || !s.Success || s.Username != "smoke-admin" || s.AccessToken == "" {
				t.Fatalf("login of %s with %q: %d %s", username, password, status, raw)
			}
		}
	}
	for _, username := range []string{"", "root", "smoke-admin2", "somebody"} {
		if status, raw, _ := do(t, p, "POST", "/api/auth/login", "", map[string]any{"username": username, "password": "whatever-it-is"}); status != http.StatusUnauthorized {
			t.Fatalf("login of %q: %d %s, want 401", username, status, raw)
		}
	}
	if status, _, _ := do(t, p, "POST", "/api/auth/login", "", nil); status == http.StatusOK {
		t.Fatal("a login without a body gets in")
	}
}

// La página de entrada guarda, bajo las claves del motor del front, la sesión del perfil.
func TestEnter_StoresWhatTheFrontReads(t *testing.T) {
	p := publisher(t)
	status, raw, header := do(t, p, "GET", "/api/smoke/enter?as=admin", "", nil)
	if status != http.StatusOK || !strings.HasPrefix(header.Get("Content-Type"), "text/html") || header.Get("Cache-Control") != "no-store" {
		t.Fatalf("enter: %d %v", status, header)
	}
	html := string(raw)
	for _, piece := range []string{"sessionStorage.setItem('auth-session'", "sessionStorage.setItem('auth-token'", "sessionStorage.setItem('auth-refresh-token'",
		"location.replace('/')", `"username":"smoke-admin"`, `"accessToken":"`, `"refreshToken":"`, "fingerprint"} {
		if !strings.Contains(html, piece) {
			t.Errorf("the page has no %s", piece)
		}
	}
	// El token de la página vale para el anfitrión.
	_, rest, _ := strings.Cut(html, `"accessToken":"`)
	token, _, _ := strings.Cut(rest, `"`)
	if status, body, _ := do(t, p, "GET", "/api/auth/context", token, nil); status != http.StatusOK {
		t.Fatalf("the token of the page: %d %s", status, body)
	}
	if status, _, _ := do(t, p, "GET", "/api/smoke/enter?as=nobody", "", nil); status != http.StatusBadRequest {
		t.Fatalf("a profile there is not: %d, want 400", status)
	}
}

// La clave que firma se crea en cada arranque: una sesión de un publicador no es nada para otro.
func TestKey_IsNewOnEveryStart(t *testing.T) {
	one, other := publisher(t), publisher(t)
	_, raw, _ := do(t, one, "GET", "/api/smoke/token", "", nil)
	s := session(t, raw)
	if status, _, _ := do(t, one, "GET", "/api/auth/context", s.AccessToken, nil); status != http.StatusOK {
		t.Fatalf("its own session: %d", status)
	}
	if status, _, _ := do(t, other, "GET", "/api/auth/context", s.AccessToken, nil); status != http.StatusUnauthorized {
		t.Fatalf("the session of another publisher: %d, want 401", status)
	}
}

// La semilla se carga una vez, sobre una base de datos como la de cmd/karpo-smoke, y lo que carga
// basta para recorrer la guía (docs/SMOKE-MANUAL.md) de punta a punta por las rutas del anfitrión:
// pedido, albarán, factura emitida desde él, su vencimiento, su cobro y los asientos.
func TestSeed_LoadsWhatTheWalkNeeds(t *testing.T) {
	noCredentials(t)
	p, err := smoke.New(context.Background(), smoke.Development, database(t), smoke.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seeded, said, err := p.Seed(context.Background())
	if err != nil || seeded.Company == "" || seeded.Customer == "" || seeded.Product == "" || seeded.Warehouse == "" || seeded.Series == "" || seeded.Terms == "" ||
		!strings.Contains(said, "$company   = '"+seeded.Company+"'") || !strings.Contains(said, "$series    = '"+seeded.Series+"'") {
		t.Fatalf("seed: %+v %q %v", seeded, said, err)
	}
	if again, said, err := p.Seed(context.Background()); err != nil || again != (smoke.Seeded{}) || !strings.Contains(said, "nothing was loaded") {
		t.Fatalf("seed again: %+v %q %v", again, said, err)
	}

	_, raw, _ := do(t, p, "GET", "/api/smoke/token", "", nil)
	token := session(t, raw).AccessToken
	call := func(method, path string, body any, want int, out any) {
		t.Helper()
		status, raw, _ := do(t, p, method, path, token, body)
		if status != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, status, raw, want)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v: %s", method, path, err, raw)
			}
		}
	}
	deliver := func() {
		t.Helper()
		if _, err := p.Host.Deliver(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// El pedido de 10 cajas, servido entero con un albarán.
	var order struct {
		ID string `json:"id"`
	}
	call("POST", "/api/orders/orders", map[string]any{"company": seeded.Company, "customer": seeded.Customer, "warehouse": seeded.Warehouse}, 201, &order)
	call("POST", "/api/orders/orders/"+order.ID+"/lines", map[string]any{"product": seeded.Product, "quantity": "10"}, 200, nil)
	call("POST", "/api/orders/orders/"+order.ID+"/confirm", map[string]any{}, 200, nil)
	deliver()
	// Inventory reservó las existencias del pedido.
	var confirmed struct {
		Lines []struct {
			Reserved string `json:"reserved"`
			Short    string `json:"short"`
		} `json:"lines"`
	}
	call("GET", "/api/orders/orders/"+order.ID, nil, 200, &confirmed)
	if len(confirmed.Lines) != 1 || confirmed.Lines[0].Reserved != "10" || confirmed.Lines[0].Short != "0" {
		t.Fatalf("the reservation: %+v", confirmed.Lines)
	}
	var note struct {
		ID string `json:"id"`
	}
	call("POST", "/api/orders/orders/"+order.ID+"/deliveries", map[string]any{}, 201, &note)
	deliver()

	// Billing preparó el borrador desde el albarán: se emite en la serie sembrada.
	var drafts struct {
		Items []bilapp.InvoiceDTO `json:"items"`
	}
	call("GET", "/api/billing/invoices?seller="+seeded.Company+"&status=draft", nil, 200, &drafts)
	if len(drafts.Items) != 1 || drafts.Items[0].SourceType != "orders.delivery" || drafts.Items[0].Net != "125.00" {
		t.Fatalf("the draft from the delivery note: %+v", drafts.Items)
	}
	var invoice bilapp.InvoiceDTO
	call("POST", "/api/billing/invoices/"+drafts.Items[0].ID+"/issue", map[string]any{"series": seeded.Series}, 200, &invoice)
	if invoice.Status != "issued" || len(invoice.IssueDate) < 4 || invoice.Number != smoke.SeriesCode+"-"+invoice.IssueDate[:4]+"-000001" ||
		invoice.Taxes == nil || invoice.Taxes.Total != "151.25" || invoice.SellerNIF != smoke.CompanyTaxID || invoice.CustomerNIF != smoke.CustomerTaxID ||
		invoice.CustomerAddress == nil {
		t.Fatalf("issued: %+v", invoice)
	}
	deliver()
	// El albarán queda facturado.
	var invoiced struct {
		InvoiceNumber string `json:"invoiceNumber"`
	}
	call("GET", "/api/orders/deliveries/"+note.ID, nil, 200, &invoiced)
	if invoiced.InvoiceNumber != invoice.Number {
		t.Fatalf("the invoiced delivery note: %+v, want %s", invoiced, invoice.Number)
	}

	// Su vencimiento, a 30 días, y su cobro por transferencia.
	var receivables struct {
		Items []recapp.ReceivableDTO `json:"items"`
	}
	call("GET", "/api/receivables/invoices?seller="+seeded.Company, nil, 200, &receivables)
	if len(receivables.Items) != 1 || receivables.Items[0].Open != "151.25" || len(receivables.Items[0].Installments) != 1 ||
		receivables.Items[0].Installments[0].Due != invoiceDue(t, invoice.IssueDate) {
		t.Fatalf("receivable: %+v", receivables.Items)
	}
	call("POST", "/api/receivables/collections", map[string]any{"seller": seeded.Company, "payer": seeded.Customer, "date": invoice.IssueDate,
		"amount": "151.25", "method": "transfer", "allocations": []map[string]any{{"invoice": invoice.ID, "installment": 1, "amount": "151.25"}}}, 201, nil)
	deliver()

	// Los dos asientos: la factura y el cobro.
	var rows []accapp.BalanceRow
	call("GET", "/api/accounting/trial-balance?company="+seeded.Company, nil, 200, &rows)
	balances := map[string]string{}
	for _, r := range rows {
		balances[r.Account] = r.Balance
	}
	for account, want := range map[string]string{"4300": "0.00", "5720": "151.25", "7000": "-125.00", "4770": "-26.25"} {
		if balances[account] != want {
			t.Fatalf("balance of %s: %q, want %s (all: %v)", account, balances[account], want, balances)
		}
	}
	// Y nada esperando a asentarse.
	var parked struct {
		Total int64 `json:"total"`
	}
	call("GET", "/api/accounting/parked?company="+seeded.Company, nil, 200, &parked)
	if parked.Total != 0 {
		t.Fatalf("parked: %d", parked.Total)
	}
}

// Los perfiles de la empresa sembrada: sin semilla no existen y la puerta lo dice; con ella, el
// lector lee y no escribe, el comercial vende y no administra, y ninguno sale de su empresa. Es
// la sección «Probar permisos» de la guía.
func TestProfiles_ReaderReadsAndClerkSells(t *testing.T) {
	noCredentials(t)
	p, err := smoke.New(context.Background(), smoke.Development, database(t), smoke.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, as := range []string{"reader", "clerk"} {
		for _, door := range []struct {
			method, path string
			body         any
		}{
			{"GET", "/api/smoke/token?as=" + as, nil},
			{"GET", "/api/smoke/enter?as=" + as, nil},
			{"POST", "/api/auth/login", map[string]any{"username": as, "password": "x"}},
		} {
			status, raw, _ := do(t, p, door.method, door.path, "", door.body)
			if status != http.StatusConflict || !strings.Contains(string(raw), "-seed") {
				t.Fatalf("%s %s before the seed: %d %s, want 409 naming -seed", door.method, door.path, status, raw)
			}
		}
	}
	seeded, _, err := p.Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	token := func(as string) string {
		t.Helper()
		status, raw, _ := do(t, p, "GET", "/api/smoke/token?as="+as, "", nil)
		s := session(t, raw)
		if status != http.StatusOK || s.Username != "smoke-"+as || s.MustChangePassword {
			t.Fatalf("token of %s: %d %s", as, status, raw)
		}
		return s.AccessToken
	}
	reader, clerk := token("reader"), token("clerk")
	call := func(who, method, path string, body any, want int, out any) {
		t.Helper()
		status, raw, _ := do(t, p, method, path, who, body)
		if status != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, status, raw, want)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				t.Fatalf("%s %s: %v: %s", method, path, err, raw)
			}
		}
	}
	deliver := func() {
		t.Helper()
		if _, err := p.Host.Deliver(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// Ninguno es administrador global.
	for _, who := range []string{reader, clerk} {
		var r authz.Resolution
		call(who, "GET", "/api/auth/context", nil, 200, &r)
		if r.Context == nil || r.Context.GlobalAdmin || len(r.Context.Grants) != 1 || r.Context.Grants[0].OrganizationID.String() != seeded.Company {
			t.Fatalf("context: %+v", r.Context)
		}
	}
	// El lector lee y no escribe.
	call(reader, "GET", "/api/parties/"+seeded.Company, nil, 200, nil)
	call(reader, "POST", "/api/orders/orders", map[string]any{"company": seeded.Company, "customer": seeded.Customer, "warehouse": seeded.Warehouse}, 403, nil)
	// El comercial vende: pedido, albarán, factura emitida, cobro.
	var order struct {
		ID string `json:"id"`
	}
	call(clerk, "POST", "/api/orders/orders", map[string]any{"company": seeded.Company, "customer": seeded.Customer, "warehouse": seeded.Warehouse}, 201, &order)
	call(clerk, "POST", "/api/orders/orders/"+order.ID+"/lines", map[string]any{"product": seeded.Product, "quantity": "10"}, 200, nil)
	call(clerk, "POST", "/api/orders/orders/"+order.ID+"/confirm", map[string]any{}, 200, nil)
	deliver()
	call(clerk, "POST", "/api/orders/orders/"+order.ID+"/deliveries", map[string]any{}, 201, nil)
	deliver()
	var drafts struct {
		Items []bilapp.InvoiceDTO `json:"items"`
	}
	call(reader, "GET", "/api/billing/invoices?seller="+seeded.Company+"&status=draft", nil, 200, &drafts)
	if len(drafts.Items) != 1 {
		t.Fatalf("drafts: %+v", drafts.Items)
	}
	call(reader, "POST", "/api/billing/invoices/"+drafts.Items[0].ID+"/issue", map[string]any{"series": seeded.Series}, 403, nil)
	var invoice bilapp.InvoiceDTO
	call(clerk, "POST", "/api/billing/invoices/"+drafts.Items[0].ID+"/issue", map[string]any{"series": seeded.Series}, 200, &invoice)
	deliver()
	call(clerk, "POST", "/api/receivables/collections", map[string]any{"seller": seeded.Company, "payer": seeded.Customer, "date": invoice.IssueDate,
		"amount": "151.25", "method": "transfer", "allocations": []map[string]any{{"invoice": invoice.ID, "installment": 1, "amount": "151.25"}}}, 201, nil)
	// El comercial no administra usuarios.
	call(clerk, "GET", "/api/security/users", nil, 403, nil)
	// Y ninguno sale de su empresa: otra empresa interna, que da de alta el administrador, no es suya.
	_, raw, _ := do(t, p, "GET", "/api/smoke/token?as=admin", "", nil)
	admin := session(t, raw).AccessToken
	var other struct {
		ID string `json:"id"`
	}
	call(admin, "POST", "/api/organizations", map[string]any{"legalName": "Otra de Humo, S.L.", "roles": []string{pardomain.RoleInternalOrganization.String()}}, 201, &other)
	call(admin, "POST", "/api/billing/series", map[string]any{"seller": other.ID, "code": smoke.SeriesCode, "year": 2026}, 201, nil)
	var series []bilapp.SeriesDTO
	call(reader, "GET", "/api/billing/series?seller="+seeded.Company, nil, 200, &series)
	if len(series) != 1 {
		t.Fatalf("the series of its company: %+v", series)
	}
	call(reader, "GET", "/api/billing/series?seller="+other.ID, nil, 200, &series)
	if len(series) != 0 {
		t.Fatalf("the series of another company: %+v", series)
	}
	call(clerk, "POST", "/api/billing/series", map[string]any{"seller": other.ID, "code": "FB", "year": 2026}, 404, nil)
}

// invoiceDue es el vencimiento de una factura emitida en issued con las condiciones sembradas: 30
// días después.
func invoiceDue(t *testing.T, issued string) string {
	t.Helper()
	d, err := time.Parse("2006-01-02", issued)
	if err != nil {
		t.Fatal(err)
	}
	return d.AddDate(0, 0, smoke.TermsDays).Format("2006-01-02")
}

// Lo que dice al arrancar no se puede tomar por otra cosa.
func TestBanner_SaysWhatItIs(t *testing.T) {
	b := smoke.Banner("127.0.0.1:8092", "a file")
	for _, piece := range []string{"NO AUTHENTICATION", "Never deploy it", "smoke-admin", "smoke-reader", "smoke-clerk", "http://127.0.0.1:8092"} {
		if !strings.Contains(b, piece) {
			t.Errorf("the banner has no %q", piece)
		}
	}
}

// Sobre una base que ya existe (PostgreSQL), el publicador sólo arranca si no tiene nada, y lo que
// dice al negarse nombra la base y algunos de sus objetos.
func TestCheckEmpty_OnlyADatabaseWithoutTables(t *testing.T) {
	for _, none := range [][]string{nil, {}} {
		if err := smoke.CheckEmpty("the database x", none); err != nil {
			t.Fatalf("no tables: %v", err)
		}
	}
	for says, objects := range map[string][]string{"has 1 object (": {"public.users"}, "has 4 objects (": {"a.one", "b.two", "c.three", "d.four"}} {
		err := smoke.CheckEmpty("the database x", objects)
		if err == nil || !strings.Contains(err.Error(), "the database x") || !strings.Contains(err.Error(), objects[0]) ||
			!strings.Contains(err.Error(), says) || !strings.Contains(err.Error(), "never deletes anything") {
			t.Fatalf("%v: %v, want it to say %q", objects, err, says)
		}
	}
}
