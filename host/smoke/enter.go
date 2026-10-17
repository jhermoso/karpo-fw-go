//go:build smoke

package smoke

import (
	"encoding/json"
	"net/http"
	"strings"
)

// La página de entrada guarda lo que guarda el motor del front de Karpo tras un login que salió
// bien, y bajo las mismas claves, todas en sessionStorage (core/services/auth.service.ts del
// motor, como lo lee la publicadora de humo de Theros):
//
//	auth-refresh-token  el token de refresco, tal como llegó
//	auth-session        {user, token, timestamp, fingerprint}, JSON
//	auth-token          el token de acceso, tal como llegó
//
// La huella es el djb2 del user agent, el idioma y el tamaño de la pantalla: la página la calcula
// como el motor. sessionStorage es de un origen y de una pestaña, así que la página se ha de abrir
// desde el origen del front, en la pestaña que se vaya a usar.
//
// Karpo en Go no tiene front propio: la página está aquí para que las cuatro publicadoras de humo
// tengan la misma puerta, y el día que se ponga un front delante de este anfitrión no haya que
// cambiar nada.

// entry es lo que se da a la página de entrada: los dos tokens y el usuario que guarda el motor.
type entry struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	User         entryUser `json:"user"`
}

// entryUser es el User del motor, sin lastLogin, que la página fija al momento en que se ejecuta.
// El token del anfitrión dice quién y no qué puede hacer: permissions es una lista vacía.
type entryUser struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	DisplayName string   `json:"displayName"`
	Position    string   `json:"position"`
	Permissions []string `json:"permissions"`
}

// page es la página de entrada. %DATA% es la entrada, como JSON: encoding/json escapa <, > y &,
// así que no puede cerrar el script.
const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Smoke entry</title>
</head>
<body>
<p id="status">Smoke publisher: entering as <strong>%WHO%</strong>, without credentials. Development only.</p>
<noscript>This page needs JavaScript: it stores the session in the browser.</noscript>
<script>
(function () {
  var data = %DATA%;
  function fingerprint() {
    var raw = [navigator.userAgent, navigator.language, screen.width, screen.height].join('|');
    var hash = 5381;
    for (var i = 0; i < raw.length; i++) {
      hash = (hash << 5) + hash + raw.charCodeAt(i);
    }
    return (hash >>> 0).toString(36);
  }
  try {
    var user = data.user;
    user.lastLogin = new Date().toISOString();
    var session = { user: user, token: data.accessToken, timestamp: Date.now(), fingerprint: fingerprint() };
    sessionStorage.setItem('auth-refresh-token', data.refreshToken);
    sessionStorage.setItem('auth-session', JSON.stringify(session));
    sessionStorage.setItem('auth-token', data.accessToken);
    location.replace('/');
  } catch (error) {
    document.getElementById('status').textContent = 'The session could not be stored: ' + error;
  }
})();
</script>
</body>
</html>
`

// enter responde la página de entrada de un perfil.
func (p *Publisher) enter(w http.ResponseWriter, r *http.Request) {
	who, known := asked(w, r)
	if !known {
		return
	}
	s, err := p.Open(who)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.Marshal(entry{AccessToken: s.AccessToken, RefreshToken: s.RefreshToken, User: entryUser{
		ID: s.UserID, Username: who.Username, Role: who.Role, DisplayName: who.DisplayName, Position: who.Position, Permissions: []string{}}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(strings.NewReplacer("%WHO%", who.Username, "%DATA%", string(data)).Replace(page)))
}
