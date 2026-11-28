package distribution

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"time"
)

// StaticFile sirve un fichero público y grande (el PMTiles de delimitaciones) con peticiones de
// rango, ETag y caché compartida. No pasa por la cadena de permiso: los datos son públicos y un
// JWT impediría la caché.
//
// El fichero se puede sustituir en caliente con un rename atómico: cada petición abre el fichero
// y, si su tamaño o su fecha cambiaron, recalcula el ETag (SHA-256) sobre el mismo descriptor que
// sirve. Una lectura en curso termina con el fichero antiguo.
type StaticFile struct {
	path, ctype string

	mu   sync.Mutex
	size int64
	mod  time.Time
	etag string
}

// StaticWriteTimeout es el plazo de escritura de una respuesta de StaticFile, que amplía sólo
// para ella el WriteTimeout general del servidor.
const StaticWriteTimeout = 10 * time.Minute

// NewStaticFile prepara el servicio de path; falla si el fichero no existe o no se puede leer.
func NewStaticFile(path, contentType string) (*StaticFile, error) {
	s := &StaticFile{path: path, ctype: contentType}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, _, err := s.tag(f); err != nil {
		return nil, err
	}
	return s, nil
}

// tag devuelve los datos y el ETag del fichero abierto, recalculándolo si cambió.
func (s *StaticFile) tag(f *os.File) (fs.FileInfo, string, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if info.IsDir() {
		return nil, "", errors.New(s.path + " is a directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.etag != "" && info.Size() == s.size && info.ModTime().Equal(s.mod) {
		return info, s.etag, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	s.size, s.mod, s.etag = info.Size(), info.ModTime(), `"sha256-`+hex.EncodeToString(h.Sum(nil))+`"`
	return info, s.etag, nil
}

// ServeHTTP atiende GET y HEAD.
func (s *StaticFile) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f, err := os.Open(s.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, etag, err := s.tag(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	h.Set("Content-Type", s.ctype)
	h.Set("Accept-Ranges", "bytes")
	// Un writer que no admite plazos (un envoltorio sin Unwrap) se queda con el general.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(StaticWriteTimeout))
	http.ServeContent(w, r, "", info.ModTime(), f)
}
