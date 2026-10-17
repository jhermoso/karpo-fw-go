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

	_ "modernc.org/sqlite"

	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
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

// La semilla se carga una vez.
func TestSeed_LoadsOnce(t *testing.T) {
	p := publisher(t)
	said, err := p.Seed()
	if err != nil || !strings.Contains(said, "1 organization") {
		t.Fatalf("seed: %q %v", said, err)
	}
	if said, err = p.Seed(); err != nil || !strings.Contains(said, "nothing was loaded") {
		t.Fatalf("seed again: %q %v", said, err)
	}
}

// Lo que dice al arrancar no se puede tomar por otra cosa.
func TestBanner_SaysWhatItIs(t *testing.T) {
	b := smoke.Banner("127.0.0.1:8092", "a file")
	for _, piece := range []string{"NO AUTHENTICATION", "Never deploy it", "smoke-admin", "http://127.0.0.1:8092"} {
		if !strings.Contains(b, piece) {
			t.Errorf("the banner has no %q", piece)
		}
	}
}
