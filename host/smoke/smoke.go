//go:build smoke

// Package smoke es la publicadora de humo de Karpo: el mismo anfitrión que sirve cmd/karpo, con una
// puerta que deja entrar a una persona SIN CREDENCIALES, para probar el conjunto desde un navegador
// o desde curl sin que nadie lea y teclee una contraseña.
//
//	GET  /api/smoke/enter?as=<profile>  una página que guarda en el navegador lo que guarda el
//	                                    motor del front tras un login, y va a /
//	GET  /api/smoke/token?as=<profile>  la misma sesión como JSON, para curl
//	POST /api/auth/login                cualquier contraseña, para los perfiles fijos
//
// Todo lo demás es el anfitrión tal cual: sus rutas, sus permisos, su refresco de sesiones.
//
// Quien entra es una de unas pocas identidades fijadas en este fichero (Profiles). Aquí nada
// autentica a nadie, así que nunca debe salir de una máquina de desarrollo, y tres cosas lo
// mantienen ahí:
//
//   - Cada fichero suyo lleva la etiqueta de build smoke: sin -tags smoke no está en ningún binario,
//     y cmd/karpo-smoke no compila. Ni cmd/karpo ni cmd/karpo-postgres lo importan.
//   - New se niega a arrancar salvo que KARPO_ENV sea exactamente "development", Listen rechaza
//     toda dirección que no sea de loopback, y cada petición tiene que nombrar esta máquina en su Host.
//   - Una prueba que siempre se ejecuta (pkg/testing/archtest, sin la etiqueta) falla si un fichero
//     Go sin la etiqueta importa este paquete o nombra sus rutas.
//
// No hay secreto que guardar: la clave que firma las sesiones y la contraseña de cada perfil se
// crean al arrancar el publicador, viven en su memoria y desaparecen cuando se para. Por eso sólo
// arranca sobre una base de datos que nadie administra aún: en el siguiente arranque nadie sabe la
// contraseña del administrador que creó el anterior.
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

// EnvVar es la variable que tiene que decir Development, exactamente, para que arranque el publicador.
const EnvVar = "KARPO_ENV"

// Development es lo que tiene que decir EnvVar.
const Development = "development"

// Profile es una de las identidades que deja entrar el publicador. Están fijadas aquí, en código:
// no hay nada que configurar ni nada que mantener en secreto.
type Profile struct {
	// Name es lo que dice ?as=.
	Name string
	// Username es el usuario del anfitrión que es. Dice smoke, para que quien mire una traza, o
	// mire quién creó un registro, vea que no es una persona.
	Username string
	// Role, DisplayName y Position son lo que guarda el motor del front del usuario de una sesión.
	Role, DisplayName, Position string
}

// Profiles son las identidades del publicador. La primera es la que quiere decir ?as= cuando no
// dice nada. Hay una: el administrador de la instalación, que puede hacerlo todo.
func Profiles() []Profile {
	return []Profile{
		{Name: "admin", Username: "smoke-admin", Role: "super", DisplayName: "Smoke Admin", Position: "Rol Técnico"},
	}
}

// profile busca un perfil por su nombre o por su username. Sin nombre es el primero.
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

// names son los nombres de los perfiles, para un mensaje.
func names() string {
	var out []string
	for _, p := range Profiles() {
		out = append(out, p.Name)
	}
	return strings.Join(out, ", ")
}

// Options es lo que decide quien arranca el publicador. Nada de ello es una credencial.
type Options struct {
	// Files guarda los ficheros de las exportaciones (nil: en memoria).
	Files expapp.Files
	// Ping dice si la base de datos responde, para la sonda de disponibilidad (nil: sin sonda).
	Ping func(context.Context) error
}

// Publisher es el anfitrión con la puerta de humo.
type Publisher struct {
	// Host es Karpo compuesto: quien arranca el publicador ejecuta sus entregas y sus tareas.
	Host *host.Host

	handler http.Handler
	inner   http.Handler
	// passwords son las de los perfiles, por username. Creadas aquí, guardadas aquí, nunca mostradas.
	passwords map[string]string
}

// CheckEnv dice si el entorno deja arrancar al publicador: KARPO_ENV exactamente "development".
// Una variable que falta es un rechazo, como cualquier otro valor.
func CheckEnv(env string) error {
	if env != Development {
		return fmt.Errorf("smoke: %s is %q and must be exactly %q: the smoke publisher lets anybody in without credentials and only starts in development", EnvVar, env, Development)
	}
	return nil
}

// random da n bytes del sistema, en hexadecimal.
func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// New compone Karpo sobre un backend migrado que nadie administra aún, crea los perfiles y añade
// la puerta. env es lo que dice KARPO_ENV: cualquier cosa que no sea "development" y no hay
// publicador.
func New(ctx context.Context, env string, sw *hotswap.Switch, o Options) (*Publisher, error) {
	if err := CheckEnv(env); err != nil {
		return nil, err
	}
	// No toma credenciales de ningún sitio: con éstas puestas el anfitrión crearía un administrador
	// propio, con una contraseña que alguien conoce.
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

// create crea el usuario de un perfil: el primer administrador de la instalación, con una
// contraseña que nadie ve, y ya cambiada una vez, para que el anfitrión no pida cambiarla antes
// de nada.
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

// call envía una petición a un handler de este proceso y decodifica una respuesta 2xx en out. Da
// el estado y, cuando no es 2xx, el cuerpo.
func (p *Publisher) call(to http.Handler, method, path, token string, body, out any) (int, []byte) {
	var sent bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&sent).Encode(body)
	}
	request := httptest.NewRequest(method, path, &sent)
	request.Host = "127.0.0.1" // de este proceso
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

// CheckAddress dice si una dirección en la que escuchar es sólo de esta máquina: una dirección IP
// de loopback (127.0.0.0/8 o ::1) con puerto. Un nombre se rechaza, localhost también: a qué se
// resuelve un nombre no se decide aquí.
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

// localHost dice si el Host de una petición nombra esta máquina: 127.0.0.1, [::1] o localhost,
// con cualquier puerto o sin él.
func localHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
	return name == "127.0.0.1" || name == "::1" || strings.EqualFold(name, "localhost")
}

// Listen escucha en una dirección, y sólo si es únicamente de esta máquina.
func (p *Publisher) Listen(address string) (net.Listener, error) {
	if err := CheckAddress(address); err != nil {
		return nil, err
	}
	return net.Listen("tcp", address)
}

// Banner es lo que dice el publicador al arrancar, para que nadie lo tome por otra cosa.
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

// ServeHTTP implementa http.Handler. Se rechaza una petición cuyo Host no nombra esta máquina:
// una página de cualquier sitio, abierta en el navegador del desarrollador, puede hacer que su
// propio nombre se resuelva a 127.0.0.1 (DNS rebinding) y entraría como cualquier otro. Su Host
// sigue diciendo ese nombre. El proxy de desarrollo del front envía el Host de su destino, que es
// esta máquina.
func (p *Publisher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !localHost(r.Host) {
		http.Error(w, "the smoke publisher only answers to 127.0.0.1, [::1] or localhost", http.StatusForbidden)
		return
	}
	p.handler.ServeHTTP(w, r)
}

// Session es lo que responde el publicador para un perfil: lo que responde el login del anfitrión
// (accessToken, refreshToken, expiresAt, userId, username, mustChangePassword), el miembro success
// que lee el login del motor del front, y qué perfil es.
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

// failure es lo que responde un login que no entra, como lo lee el motor del front.
type failure struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"errorMessage"`
}

// Open da una sesión de un perfil: a nadie se le pregunta nada. Es una sesión del anfitrión como
// cualquier otra: el anfitrión emitió sus tokens, y los refresca y los termina.
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

// asked da el perfil que pide una petición con ?as=, o responde 400 diciendo cuáles hay.
func asked(w http.ResponseWriter, r *http.Request) (Profile, bool) {
	who, known := profile(r.URL.Query().Get("as"))
	if !known {
		http.Error(w, fmt.Sprintf("there is no profile %q: the profiles are %s", r.URL.Query().Get("as"), names()), http.StatusBadRequest)
	}
	return who, known
}

// token responde la sesión de un perfil como JSON.
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

// login es el login del anfitrión con una diferencia: para los perfiles fijos (el usuario es el
// nombre de un perfil o su username) no se mira la contraseña. Cualquier otro usuario va al
// anfitrión, que le pide su contraseña como siempre.
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

// Seed carga algo que mirar, por las rutas del anfitrión y como el perfil admin: una organización
// en Parties. Si ya hay una party, no carga nada. Dice lo que hizo.
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
