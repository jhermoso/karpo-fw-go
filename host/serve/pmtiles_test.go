package serve_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/host/serve"
)

func TestMountPMTiles(t *testing.T) {
	mux := http.NewServeMux()
	if err := serve.MountPMTiles(mux, ""); err != nil {
		t.Fatal(err)
	}
	if _, pattern := mux.Handler(httptest.NewRequest("GET", serve.PMTilesPath, nil)); pattern != "" {
		t.Fatalf("sin KARPO_PMTILES la ruta está montada: %q", pattern)
	}
	err := serve.MountPMTiles(http.NewServeMux(), filepath.Join(t.TempDir(), "no.pmtiles"))
	if err == nil || !strings.Contains(err.Error(), "KARPO_PMTILES") {
		t.Fatalf("una ruta inexistente: %v", err)
	}
	path := filepath.Join(t.TempDir(), "karpo.pmtiles")
	_ = os.WriteFile(path, []byte("PMTiles"), 0o600)
	mux = http.NewServeMux()
	if err := serve.MountPMTiles(mux, path); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", serve.PMTilesPath, nil))
	if w.Code != 200 || w.Body.String() != "PMTiles" {
		t.Fatalf("GET: %d", w.Code)
	}
}

func TestLoad_ReadsPMTiles(t *testing.T) {
	t.Setenv("KARPO_JWT_SECRET", strings.Repeat("s", serve.MinSecret))
	t.Setenv("KARPO_PMTILES", "/data/karpo.pmtiles")
	c, err := serve.Load(t.TempDir())
	if err != nil || c.PMTiles != "/data/karpo.pmtiles" {
		t.Fatalf("%+v %v", c, err)
	}
}
