//go:build smoke

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// out es lo que escribe el programa, leído mientras se ejecuta.
type out struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (o *out) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}

func (o *out) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

func clean(t *testing.T) {
	t.Helper()
	t.Setenv("KARPO_BOOTSTRAP_ADMIN_USER", "")
	t.Setenv("KARPO_BOOTSTRAP_ADMIN_PASSWORD", "")
}

// Fuera de desarrollo el programa se para antes de crear o abrir nada.
func TestRun_RefusesOutsideDevelopment(t *testing.T) {
	clean(t)
	for _, env := range []string{"", "production", "Development"} {
		db := filepath.Join(t.TempDir(), "karpo.db")
		err := run(context.Background(), []string{"-port", "0", "-db", db}, env, &out{}, nil)
		if err == nil || !strings.Contains(err.Error(), "KARPO_ENV") {
			t.Fatalf("KARPO_ENV=%q: %v", env, err)
		}
		if _, statErr := os.Stat(db); statErr == nil {
			t.Fatalf("KARPO_ENV=%q: the database was created", env)
		}
	}
}

// En desarrollo sirve en 127.0.0.1, deja entrar al perfil y se para cuando se le dice.
func TestRun_ServesOnThisMachineAndLetsIn(t *testing.T) {
	clean(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := filepath.Join(t.TempDir(), "karpo.db")
	written := &out{}
	listening, done := make(chan string, 1), make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-port", "0", "-db", db, "-seed"}, "development", written, listening)
	}()

	var address string
	select {
	case address = <-listening:
	case err := <-done:
		t.Fatalf("it did not start: %v\n%s", err, written)
	case <-time.After(2 * time.Minute):
		t.Fatal("it did not start in two minutes")
	}
	if !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("it listens on %s", address)
	}
	said := written.String()
	for _, piece := range []string{"NO AUTHENTICATION", "1 organization was loaded", address} {
		if !strings.Contains(said, piece) {
			t.Errorf("it does not say %q:\n%s", piece, said)
		}
	}
	// Nunca se escribe ni la clave ni una contraseña.
	for _, word := range []string{"password", "secret", "Bearer"} {
		if strings.Contains(strings.ToLower(said), strings.ToLower(word)) {
			t.Errorf("it writes %q:\n%s", word, said)
		}
	}

	res, err := http.Get("http://" + address + "/api/smoke/token?as=admin")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		AccessToken string `json:"accessToken"`
		Username    string `json:"username"`
	}
	err = json.NewDecoder(res.Body).Decode(&s)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || s.AccessToken == "" || s.Username != "smoke-admin" {
		t.Fatalf("token: %d %+v %v", res.StatusCode, s, err)
	}
	req, _ := http.NewRequest("GET", "http://"+address+"/api/parties", nil)
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a route of a context: %d", res.StatusCode)
	}
	res, err = http.Get("http://" + address + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("readyz: %d", res.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("it stopped badly: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("it did not stop")
	}

	// El mismo fichero otra vez: existe, así que ni siquiera se abre.
	err = run(context.Background(), []string{"-port", "0", "-db", db}, "development", &out{}, nil)
	if err == nil || !strings.Contains(err.Error(), "new database") {
		t.Fatalf("it started again on the same database: %v", err)
	}
}

// Un fichero que ya está ahí nunca se toca: podría ser la base de datos de una instalación real
// todavía sin administrador, y el publicador dejaría allí el suyo para siempre.
func TestRun_RefusesADatabaseThatAlreadyExists(t *testing.T) {
	clean(t)
	// Un fichero vacío es una base de datos que SQLite abre y migra; el otro no lo es en absoluto.
	for _, before := range [][]byte{{}, []byte("whatever is here is not of the smoke publisher")} {
		dir := t.TempDir()
		db := filepath.Join(dir, "karpo.db")
		if err := os.WriteFile(db, before, 0o644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		err := run(ctx, []string{"-port", "0", "-db", db}, "development", &out{}, nil)
		cancel()
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("it started on a file that was there: %v", err)
		}
		after, err := os.ReadFile(db)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("the file was changed: %d bytes, %v", len(after), err)
		}
		// Tampoco se puso nada a su lado (el diario de SQLite, la carpeta de las exportaciones).
		if around, _ := os.ReadDir(dir); len(around) != 1 {
			t.Fatalf("something was created next to the file: %v", around)
		}
	}
}
