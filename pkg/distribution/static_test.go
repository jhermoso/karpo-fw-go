package distribution_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
)

func staticFixture(t *testing.T, body string) (string, *distribution.StaticFile) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "karpo.pmtiles")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := distribution.NewStaticFile(path, "application/vnd.pmtiles")
	if err != nil {
		t.Fatal(err)
	}
	return path, s
}

func serve(h http.Handler, method string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/geo/karpo.pmtiles", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const pmtiles = "PMTiles\x03 cabecera de prueba y algo mas de contenido!"

func TestStaticFile_ServesRangesWithCache(t *testing.T) {
	_, s := staticFixture(t, pmtiles)
	w := serve(s, http.MethodGet)
	h := w.Header()
	if w.Code != 200 || w.Body.String() != pmtiles || h.Get("Accept-Ranges") != "bytes" || !strings.HasPrefix(h.Get("ETag"), `"sha256-`) ||
		h.Get("Cache-Control") != "public, max-age=86400, stale-while-revalidate=604800" || h.Get("Content-Type") != "application/vnd.pmtiles" ||
		h.Get("Content-Length") != strconv.Itoa(len(pmtiles)) {
		t.Fatalf("GET: %d %v %q", w.Code, h, w.Body.String())
	}
	etag := h.Get("ETag")
	if w := serve(s, http.MethodGet, "Range", "bytes=0-15"); w.Code != 206 || w.Body.String() != pmtiles[:16] ||
		w.Header().Get("Content-Range") != "bytes 0-15/"+strconv.Itoa(len(pmtiles)) {
		t.Fatalf("rango: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if w := serve(s, http.MethodGet, "Range", "bytes=1000-2000"); w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("rango fuera: %d", w.Code)
	}
	if w := serve(s, http.MethodGet, "If-None-Match", etag); w.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match: %d", w.Code)
	}
	if w := serve(s, http.MethodGet, "Range", "bytes=0-15", "If-Range", `"otro"`); w.Code != 200 || w.Body.Len() != len(pmtiles) {
		t.Fatalf("If-Range distinto: %d", w.Code)
	}
	if w := serve(s, http.MethodGet, "Range", "bytes=0-15", "If-Range", etag); w.Code != 206 {
		t.Fatalf("If-Range igual: %d", w.Code)
	}
	if w := serve(s, http.MethodHead); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("ETag") != etag {
		t.Fatalf("HEAD: %d %d", w.Code, w.Body.Len())
	}
	if w := serve(s, http.MethodPost); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d %v", w.Code, w.Header())
	}
}

func TestStaticFile_NewFileNewETag(t *testing.T) {
	path, s := staticFixture(t, pmtiles)
	old := serve(s, http.MethodGet).Header().Get("ETag")
	next := path + ".new"
	if err := os.WriteFile(next, []byte(strings.ToUpper(pmtiles)), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(next, later, later)
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	w := serve(s, http.MethodGet, "Range", "bytes=0-15", "If-Range", old)
	if w.Code != 200 || w.Header().Get("ETag") == old || w.Body.String() != strings.ToUpper(pmtiles) {
		t.Fatalf("fichero nuevo con el ETag antiguo: %d %v", w.Code, w.Header())
	}
	// Mismo tamaño y misma fecha: el ETag se conserva sin recalcular.
	if again := serve(s, http.MethodGet).Header().Get("ETag"); again != w.Header().Get("ETag") {
		t.Fatalf("el ETag cambia sin cambiar el fichero")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if w := serve(s, http.MethodGet); w.Code != 404 {
		t.Fatalf("fichero borrado: %d", w.Code)
	}
}

func TestNewStaticFile_FailsWithoutTheFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := distribution.NewStaticFile(filepath.Join(dir, "no.pmtiles"), "x"); err == nil {
		t.Fatal("un fichero que no existe")
	}
	if _, err := distribution.NewStaticFile(dir, "x"); err == nil {
		t.Fatal("un directorio")
	}
}

// deadlineRecorder registra el plazo de escritura que pide el manejador.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error { d.deadline = t; return nil }

func TestStaticFile_ExtendsTheWriteDeadline(t *testing.T) {
	_, s := staticFixture(t, pmtiles)
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/geo/karpo.pmtiles", nil))
	if until := time.Until(w.deadline); until < distribution.StaticWriteTimeout-time.Minute {
		t.Fatalf("plazo de escritura: %v", until)
	}
}

func TestStaticFile_CORSExposesRangeHeaders(t *testing.T) {
	_, s := staticFixture(t, pmtiles)
	srv := httptest.NewServer(distribution.CORSOrigins([]string{"https://app.example.com"})(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Range", "bytes=0-6")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	exposed := res.Header.Get("Access-Control-Expose-Headers")
	if res.StatusCode != 206 || string(body) != "PMTiles" || !strings.Contains(exposed, "Content-Range") || !strings.Contains(exposed, "ETag") {
		t.Fatalf("CORS: %d %q %q", res.StatusCode, exposed, body)
	}
}
