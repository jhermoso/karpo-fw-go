//go:build smoke

// Package smoke is the smoke publisher of Karpo: the same host cmd/karpo serves, with a door that
// lets a person in WITHOUT CREDENTIALS, to try the whole from a browser or from curl without
// anybody reading and typing a password.
//
//	GET  /api/smoke/enter?as=<profile>  a page that stores in the browser what the front engine
//	                                    stores after a login, and goes to /
//	GET  /api/smoke/token?as=<profile>  the same session as JSON, for curl
//	POST /api/auth/login                any password, for the fixed profiles
//
// Everything else is the host as it is: its routes, its permissions, its refresh of sessions.
//
// Who gets in is one of a few identities fixed in this file (Profiles). Nothing here
// authenticates anybody, so it must never leave a development machine, and three things keep it
// there:
//
//   - Every file of it carries the build tag smoke: without -tags smoke it is not in any binary,
//     and cmd/karpo-smoke does not compile. Neither cmd/karpo nor cmd/karpo-postgres import it.
//   - New refuses to start unless KARPO_ENV is exactly "development", Listen refuses any
//     address that is not a loopback one, and every request has to name this machine in its Host.
//   - A test that always runs (pkg/testing/archtest, without the tag) fails if a Go file without
//     the tag imports this package or names its routes.
//
// There is no secret to keep: the key that signs the sessions and the password of each profile
// are made when the publisher starts, live in its memory and are gone when it stops. That is why
// it only starts on a database nobody administers yet: on the next start nobody knows the
// password of the administrator the previous one made.
package smoke

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	secapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// EnvVar is the variable that has to say Development, exactly, for the publisher to start.
const EnvVar = "KARPO_ENV"

// Development is what EnvVar has to say.
const Development = "development"

// Profile is one of the identities the publisher lets in. They are fixed here, in code: there
// is nothing to configure and nothing to keep secret.
type Profile struct {
	// Name is what ?as= says.
	Name string
	// Username is the user of the host it is. It says smoke, so that who looks at a trace, or at
	// who created a record, sees it is not a person.
	Username string
	// Role, DisplayName and Position are what the front engine keeps of the user of a session.
	Role, DisplayName, Position string
}

// Profiles are the identities of the publisher. The first is the one ?as= means when it says
// nothing. There is one: the administrator of the installation, who may do everything.
func Profiles() []Profile {
	return []Profile{
		{Name: "admin", Username: "smoke-admin", Role: "super", DisplayName: "Smoke Admin", Position: "Rol Técnico"},
	}
}

// profile finds a profile by its name or by its username. No name is the first one.
func profile(name string) (Profile, bool) {
	all := Profiles()
	if name == "" {
		return all[0], true
	}
	for _, p := range all {
		if p.Name == name || p.Username == name {
			return p, true
		}
	}
	return Profile{}, false
}

// names are the names of the profiles, for a message.
func names() string {
	var out []string
	for _, p := range Profiles() {
		out = append(out, p.Name)
	}
	return strings.Join(out, ", ")
}

// Options is what who starts the publisher decides. None of it is a credential.
type Options struct {
	// Files keeps the files of the exports (nil: in memory).
	Files expapp.Files
	// Ping tells whether the database answers, for the readiness probe (nil: no probe).
	Ping func(context.Context) error
}

// Publisher is the host with the door of smoke.
type Publisher struct {
	// Host is Karpo composed: who starts the publisher runs its deliveries and its chores.
	Host *host.Host

	handler http.Handler
	inner   http.Handler
	// passwords are those of the profiles, by username. Made here, kept here, never shown.
	passwords map[string]string
}

// CheckEnv says whether the environment lets the publisher start: KARPO_ENV exactly
// "development". A variable that is missing is a refusal, as any other value.
func CheckEnv(env string) error {
	if env != Development {
		return fmt.Errorf("smoke: %s is %q and must be exactly %q: the smoke publisher lets anybody in without credentials and only starts in development", EnvVar, env, Development)
	}
	return nil
}

// random gives n bytes of the system, as hexadecimal.
func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// New composes Karpo on a migrated backend nobody administers yet, makes the profiles and adds
// the door. env is what KARPO_ENV says: anything but "development" and there is no publisher.
func New(ctx context.Context, env string, sw *hotswap.Switch, o Options) (*Publisher, error) {
	if err := CheckEnv(env); err != nil {
		return nil, err
	}
	// It takes no credentials from anywhere: with these set the host would make an administrator
	// of its own, with a password somebody knows.
	for _, name := range []string{security.EnvBootstrapUser, security.EnvBootstrapPassword} {
		if os.Getenv(name) != "" {
			return nil, fmt.Errorf("smoke: %s is set: the smoke publisher takes no credentials, unset it", name)
		}
	}
	secret, err := random(32)
	if err != nil {
		return nil, err
	}
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte(secret), Files: o.Files})
	if err != nil {
		return nil, err
	}
	started, err := h.Start(ctx)
	if err != nil {
		return nil, err
	}
	if started.Bootstrap == secapp.BootstrapNotNeeded {
		return nil, errors.New("smoke: the database already has an administrator: the smoke publisher only starts on a new database (give -db a file that does not exist)")
	}
	p := &Publisher{Host: h, inner: h.Handler(), passwords: map[string]string{}}
	for _, who := range Profiles() {
		if err := p.create(ctx, who); err != nil {
			return nil, err
		}
	}

	mux := http.NewServeMux()
	health := distribution.NewHealthRegistry()
	if o.Ping != nil {
		health.RegisterReadinessProbe("database", o.Ping)
	}
	health.Mount(mux)
	mux.HandleFunc("GET /api/smoke/enter", p.enter)
	mux.HandleFunc("GET /api/smoke/token", p.token)
	mux.HandleFunc("POST /api/auth/login", p.login)
	mux.Handle("/", p.inner)
	p.handler = mux
	return p, nil
}

// create makes the user of a profile: the first administrator of the installation, with a
// password nobody sees, and already changed once, so that the host does not ask to change it
// before anything else.
func (p *Publisher) create(ctx context.Context, who Profile) error {
	first, err := random(24)
	if err != nil {
		return err
	}
	kept, err := random(24)
	if err != nil {
		return err
	}
	outcome, err := p.Host.Security.Service.Bootstrap.Run(ctx, who.Username, first)
	if err != nil {
		return fmt.Errorf("smoke: %s: %w", who.Username, err)
	}
	if outcome != secapp.BootstrapCreated {
		return fmt.Errorf("smoke: %s was not created: %s", who.Username, outcome)
	}
	var session secapp.Tokens
	if status, body := p.call(p.inner, "POST", "/api/auth/login", "", secapp.Login{Username: who.Username, Password: first}, &session); status != http.StatusOK {
		return fmt.Errorf("smoke: the first session of %s: %d %s", who.Username, status, body)
	}
	if status, body := p.call(p.inner, "POST", "/api/auth/change-password", session.AccessToken,
		secapp.ChangePassword{CurrentPassword: first, NewPassword: kept}, nil); status != http.StatusOK {
		return fmt.Errorf("smoke: the password of %s: %d %s", who.Username, status, body)
	}
	p.passwords[who.Username] = kept
	return nil
}

// call sends a request to a handler of this process and decodes a 2xx answer into out. It gives
// the status and, when it is not 2xx, the body.
func (p *Publisher) call(to http.Handler, method, path, token string, body, out any) (int, []byte) {
	var sent bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&sent).Encode(body)
	}
	request := httptest.NewRequest(method, path, &sent)
	request.Host = "127.0.0.1" // of this process
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	to.ServeHTTP(recorder, request)
	if recorder.Code >= 300 {
		return recorder.Code, recorder.Body.Bytes()
	}
	if out != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
			return http.StatusInternalServerError, []byte(err.Error())
		}
	}
	return recorder.Code, nil
}

// CheckAddress says whether an address to listen on is of this machine alone: a loopback IP
// address (127.0.0.0/8 or ::1) with a port. A name is refused, localhost too: what a name
// resolves to is not decided here.
func CheckAddress(address string) error {
	name, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("smoke: the address %q is not host:port: %v", address, err)
	}
	if ip := net.ParseIP(name); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("smoke: the address %q is not a loopback IP address (127.0.0.1 or ::1): the smoke publisher does not listen anywhere else", address)
}

// localHost says whether the Host of a request names this machine: 127.0.0.1, [::1] or
// localhost, with any port or none.
func localHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
	return name == "127.0.0.1" || name == "::1" || strings.EqualFold(name, "localhost")
}

// Listen listens on an address, and only if it is of this machine alone.
func (p *Publisher) Listen(address string) (net.Listener, error) {
	if err := CheckAddress(address); err != nil {
		return nil, err
	}
	return net.Listen("tcp", address)
}

// Banner is what the publisher says when it starts, so that nobody takes it for something else.
func Banner(address, database string) string {
	line := strings.Repeat("!", 78)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "!!  SMOKE PUBLISHER: NO AUTHENTICATION. ANYBODY WHO REACHES IT IS LET IN.\n")
	fmt.Fprintf(&b, "!!  For development alone. Never deploy it, never expose it.\n")
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "Karpo, every context in one process, on %s\n", database)
	fmt.Fprintf(&b, "  listening on  http://%s\n", address)
	fmt.Fprintf(&b, "  enter         GET /api/smoke/enter?as=<profile>\n")
	fmt.Fprintf(&b, "  token         GET /api/smoke/token?as=<profile>\n")
	for _, p := range Profiles() {
		fmt.Fprintf(&b, "  profile       %-7s user %s, administrator of the installation\n", p.Name, p.Username)
	}
	return b.String()
}

// ServeHTTP implements http.Handler. A request whose Host does not name this machine is refused:
// a page of anywhere, open in the browser of the developer, can make its own name resolve to
// 127.0.0.1 (DNS rebinding) and would be let in as anybody else. Its Host still says that name.
// The development proxy of the front sends the Host of its target, which is this machine.
func (p *Publisher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !localHost(r.Host) {
		http.Error(w, "the smoke publisher only answers to 127.0.0.1, [::1] or localhost", http.StatusForbidden)
		return
	}
	p.handler.ServeHTTP(w, r)
}

// Session is what the publisher answers for a profile: what the login of the host answers
// (accessToken, refreshToken, expiresAt, userId, username, mustChangePassword), the member
// success the login of the front engine reads, and which profile it is.
type Session struct {
	Success            bool      `json:"success"`
	AccessToken        string    `json:"accessToken"`
	RefreshToken       string    `json:"refreshToken"`
	ExpiresAt          time.Time `json:"expiresAt"`
	UserID             string    `json:"userId"`
	Username           string    `json:"username"`
	MustChangePassword bool      `json:"mustChangePassword"`
	Profile            string    `json:"profile"`
}

// failure is what a login that does not get in answers, as the front engine reads it.
type failure struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"errorMessage"`
}

// Open gives a session of a profile: nobody is asked anything. It is a session of the host as
// any other: the host issued its tokens, and refreshes and ends them.
func (p *Publisher) Open(who Profile) (Session, error) {
	password, known := p.passwords[who.Username]
	if !known {
		return Session{}, fmt.Errorf("smoke: there is no profile %q", who.Name)
	}
	var t secapp.Tokens
	if status, body := p.call(p.inner, "POST", "/api/auth/login", "", secapp.Login{Username: who.Username, Password: password}, &t); status != http.StatusOK {
		return Session{}, fmt.Errorf("smoke: the session of %s: %d %s", who.Username, status, body)
	}
	return Session{Success: true, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpiresAt: t.ExpiresAt, UserID: t.UserID,
		Username: t.Username, MustChangePassword: t.MustChangePassword, Profile: who.Name}, nil
}

// asked gives the profile a request asks for with ?as=, or answers 400 saying which there are.
func asked(w http.ResponseWriter, r *http.Request) (Profile, bool) {
	who, known := profile(r.URL.Query().Get("as"))
	if !known {
		http.Error(w, fmt.Sprintf("there is no profile %q: the profiles are %s", r.URL.Query().Get("as"), names()), http.StatusBadRequest)
	}
	return who, known
}

// token answers the session of a profile as JSON.
func (p *Publisher) token(w http.ResponseWriter, r *http.Request) {
	who, known := asked(w, r)
	if !known {
		return
	}
	s, err := p.Open(who)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// login is the login of the host with one difference: for the fixed profiles (the user is the
// name of a profile or its username) the password is not looked at. Any other user goes to the
// host, which asks for its password as always.
func (p *Publisher) login(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, failure{ErrorMessage: "the body must be {username, password}"})
		return
	}
	var c struct {
		Username string `json:"username"`
	}
	_ = json.Unmarshal(raw, &c)
	who, known := profile(c.Username)
	if !known || c.Username == "" {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		p.inner.ServeHTTP(w, r)
		return
	}
	s, err := p.Open(who)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, failure{ErrorMessage: "the session could not be opened"})
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Seed loads something to look at, through the routes of the host and as the profile admin: one
// organization in Parties. If there is a party already, it loads nothing. It says what it did.
func (p *Publisher) Seed() (string, error) {
	admin, _ := profile("admin")
	s, err := p.Open(admin)
	if err != nil {
		return "", err
	}
	var page struct {
		Total int64 `json:"total"`
	}
	if status, body := p.call(p, "GET", "/api/parties?size=1", s.AccessToken, nil, &page); status != http.StatusOK {
		return "", fmt.Errorf("smoke: GET /api/parties: %d %s", status, body)
	}
	if page.Total > 0 {
		return "there is data already: nothing was loaded", nil
	}
	if status, body := p.call(p, "POST", "/api/organizations", s.AccessToken, map[string]any{"legalName": "Smoke Company"}, nil); status != http.StatusCreated {
		return "", fmt.Errorf("smoke: POST /api/organizations: %d %s", status, body)
	}
	return "1 organization was loaded (Smoke Company)", nil
}
