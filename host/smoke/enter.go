//go:build smoke

package smoke

import (
	"encoding/json"
	"net/http"
	"strings"
)

// The page of entry stores what the front engine of Karpo stores after a login that went well,
// and under the same keys, all of them in sessionStorage (core/services/auth.service.ts of the
// engine, as the smoke publisher of Theros reads it):
//
//	auth-refresh-token  the refresh token, as it came
//	auth-session        {user, token, timestamp, fingerprint}, JSON
//	auth-token          the access token, as it came
//
// The fingerprint is the djb2 of the user agent, the language and the size of the screen: the
// page computes it as the engine does. sessionStorage is of an origin and of a tab, so the page
// has to be opened from the origin of the front, in the tab that will be used.
//
// Karpo in Go has no front of its own: the page is here so that the four smoke publishers have
// the same door, and the day a front is put before this host nothing has to change.

// entry is what the page of entry is given: the two tokens and the user the engine keeps.
type entry struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	User         entryUser `json:"user"`
}

// entryUser is the User of the engine, without lastLogin, which the page sets to the moment it
// runs. The token of the host says who and not what it may do: permissions is an empty list.
type entryUser struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	DisplayName string   `json:"displayName"`
	Position    string   `json:"position"`
	Permissions []string `json:"permissions"`
}

// page is the page of entry. %DATA% is the entry, as JSON: encoding/json escapes <, > and &,
// so it cannot close the script.
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

// enter answers the page of entry of a profile.
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
