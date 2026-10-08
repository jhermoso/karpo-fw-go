package security_test

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	astapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	autapp "github.com/jhermoso/karpo-fw-go/contexts/audit/application"
	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	docapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	exgapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	facapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	geoapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	invapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	modapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	ordapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	payapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	prlapp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	prdapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	purapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security/contracts"
	sdist "github.com/jhermoso/karpo-fw-go/contexts/security/distribution"
	sdomain "github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	shpapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	treapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	wrkapp "github.com/jhermoso/karpo-fw-go/contexts/work/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// provider stands for an external identity provider: its tokens are "ext:<issuer>|<subject>".
type provider struct{}

func (provider) Verify(_ context.Context, token string) (contracts.ExternalIdentity, error) {
	rest, ok := strings.CutPrefix(token, "ext:")
	if !ok {
		return contracts.ExternalIdentity{}, errors.New("not a token of this provider")
	}
	issuer, subject, _ := strings.Cut(rest, "|")
	return contracts.ExternalIdentity{Issuer: issuer, Subject: subject}, nil
}

// host is a modular monolith with Security and Parties on one hot-swappable backend. There is no
// in-memory security directory: the authorization resolver reads the Security module.
type host struct {
	t       *testing.T
	srv     *httptest.Server
	sw      *hotswap.Switch
	sec     *security.Module
	parties *parties.Module
}

const (
	bootPassword = "bootstrap-password-1"
	rootPassword = "root-password-0001"
	idp          = "https://id.example"
)

func compose(t *testing.T) *host {
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("security-test")})
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	sec := security.Compose(sw,
		security.WithTokenIssuer(sdist.HS256Issuer{JWT: jwt}),
		security.WithPasswordHasher(sinfra.NewPBKDF2(1000)), // the real cost would only slow the test down
		security.WithParties(sinfra.PartiesDirectory{Directory: pm.Directory, Organizations: pm.Organizations}),
		security.WithLockout(sdomain.LockoutPolicy{MaxAttempts: 3, Duration: 15 * time.Minute}))

	// Two ways of authenticating, one resolver: the own token and the external provider.
	authn := authorization.Authenticators(jwt, sec.Authenticator(provider{}))
	resolver := authorization.NewResolver(sec.Directory, authorization.Options{})

	protected := http.NewServeMux()
	sec.HTTP.RegisterRoutes(protected)
	pm.HTTP.RegisterRoutes(protected)
	mux := http.NewServeMux()
	sec.HTTP.RegisterPublicRoutes(mux)
	mux.Handle("/", distribution.Chain(protected, distribution.Authorize(authn, resolver)))
	srv := httptest.NewServer(distribution.Chain(mux, distribution.Correlation()))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return &host{t: t, srv: srv, sw: sw, sec: sec, parties: pm}
}

// start is what a host does on start, after migrating: declare the permissions of its contexts
// and create the bootstrap administrator.
func (h *host) start(wantAdded int) {
	h.t.Helper()
	ctx := context.Background()
	report, err := h.sec.SyncCatalog(ctx, papp.Permissions()...)
	if err != nil || report.Added != wantAdded {
		h.t.Fatalf("catalog: %+v %v", report, err)
	}
	if again, err := h.sec.SyncCatalog(ctx, papp.Permissions()...); err != nil || again != (sapp.CatalogSync{}) {
		h.t.Fatalf("the synchronization is idempotent: %+v %v", again, err)
	}
	if out, err := h.sec.Service.Bootstrap.Run(ctx, "", ""); err != nil || out != sapp.BootstrapNoCredentials {
		h.t.Fatalf("no credentials, no administrator: %q %v", out, err)
	}
	if _, err := h.sec.Service.Bootstrap.Run(ctx, "root", "short"); !errors.Is(err, fw.ErrValidation) {
		h.t.Fatalf("the bootstrap password follows the policy: %v", err)
	}
	if out, err := h.sec.Service.Bootstrap.Run(ctx, "root", bootPassword); err != nil || out != sapp.BootstrapCreated {
		h.t.Fatalf("bootstrap: %q %v", out, err)
	}
	if out, err := h.sec.Service.Bootstrap.Run(ctx, "other", bootPassword); err != nil || out != sapp.BootstrapNotNeeded {
		h.t.Fatalf("a second start creates nobody: %q %v", out, err)
	}
}

func (h *host) do(method, path, auth string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(res.Body)
	if out != nil && res.StatusCode < 300 {
		if err := json.Unmarshal(raw.Bytes(), out); err != nil {
			h.t.Fatalf("%s %s: %v: %s", method, path, err, raw.String())
		}
	}
	return res.StatusCode
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func bearer(t sapp.Tokens) string { return "Bearer " + t.AccessToken }

func (h *host) login(user, password string) (sapp.Tokens, int) {
	h.t.Helper()
	var tok sapp.Tokens
	status := h.do("POST", "/api/auth/login", "", map[string]any{"username": user, "password": password}, &tok)
	return tok, status
}

// signIn logs in a user whose password was set by an administrator, and changes it as required.
func (h *host) signIn(user, initial, password string) sapp.Tokens {
	h.t.Helper()
	tok, status := h.login(user, initial)
	h.must(status, 200, "login of "+user)
	if !tok.MustChangePassword {
		h.t.Fatalf("%s must change a password set by an administrator", user)
	}
	h.must(h.do("POST", "/api/auth/change-password", bearer(tok), map[string]any{"currentPassword": initial, "newPassword": password}, &tok),
		200, "change password of "+user)
	return tok
}

func (h *host) context(auth string) *authz.Context {
	h.t.Helper()
	var res authz.Resolution
	h.must(h.do("GET", "/api/auth/context", auth, nil, &res), 200, "context")
	if res.Outcome != authz.Allow || res.Context == nil {
		h.t.Fatalf("context: %+v", res)
	}
	return res.Context
}

func users(r string) string { return "/api/security/users" + r }

func (h *host) scenario(tag string) {
	t := h.t
	ctx := context.Background()

	// --- Bootstrap: the first administrator can only change its password ---------------------
	boot, status := h.login("ROOT", bootPassword) // user names ignore case
	h.must(status, 200, "bootstrap login")
	if !boot.MustChangePassword || boot.RefreshToken == "" {
		t.Fatalf("bootstrap tokens: %+v", boot)
	}
	// The access token says who the user is, never what it may do (C# packed up to 272 codes).
	payload, _ := base64.RawURLEncoding.DecodeString(strings.Split(boot.AccessToken, ".")[1])
	var claims map[string]any
	_ = json.Unmarshal(payload, &claims)
	if claims["sub"] != sdomain.BootstrapAdminUser.String() || claims["partyId"] != sdomain.BootstrapAdminParty.String() ||
		claims["roles"] != nil || claims["permissions"] != nil || claims["organizationScope"] != nil {
		t.Fatalf("claims: %v", claims)
	}
	if c := h.context(bearer(boot)); c.GlobalAdmin || len(c.Permissions) != 0 {
		t.Fatalf("until the password changes the context is empty: %+v", c)
	}
	h.must(h.do("GET", users(""), bearer(boot), nil, nil), 403, "must change the password first")
	h.must(h.do("POST", "/api/auth/change-password", bearer(boot), map[string]any{"currentPassword": bootPassword, "newPassword": "short"}, nil), 400, "policy")
	h.must(h.do("POST", "/api/auth/change-password", bearer(boot), map[string]any{"currentPassword": "not-the-password", "newPassword": rootPassword}, nil), 422, "wrong current password")
	h.must(h.do("POST", "/api/auth/change-password", bearer(boot), map[string]any{"currentPassword": bootPassword, "newPassword": bootPassword}, nil), 422, "same password")
	var rootTok sapp.Tokens
	h.must(h.do("POST", "/api/auth/change-password", bearer(boot), map[string]any{"currentPassword": bootPassword, "newPassword": rootPassword}, &rootTok), 200, "change")
	root := bearer(rootTok)
	if c := h.context(root); !c.GlobalAdmin {
		t.Fatalf("the bootstrap administrator is global: %+v", c)
	}
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": boot.RefreshToken}, nil), 401, "changing the password ends the other sessions")
	if _, status := h.login("root", bootPassword); status != 401 {
		t.Fatalf("the old password no longer works: %d", status)
	}
	h.must(h.do("GET", users(""), "", nil, nil), 401, "no token")

	// --- Parties works on the real directory, untouched --------------------------------------
	var acme, beta, martaP, carlosP, pedroP, annaP papp.PartyDTO
	internal := []string{pdomain.RoleInternalOrganization.String()}
	h.must(h.do("POST", "/api/organizations", root, map[string]any{"legalName": "Acme " + tag, "roles": internal}, &acme), 201, "acme")
	h.must(h.do("POST", "/api/organizations", root, map[string]any{"legalName": "Beta " + tag, "roles": internal}, &beta), 201, "beta")
	for name, p := range map[string]*papp.PartyDTO{"Marta": &martaP, "Carlos": &carlosP, "Pedro": &pedroP, "Anna": &annaP} {
		h.must(h.do("POST", "/api/persons", root, map[string]any{"givenName": name, "firstSurname": "Test " + tag}, p), 201, name)
	}

	// --- Registering users --------------------------------------------------------------------
	var marta, carlos, pedro, anna sapp.UserDTO
	newUser := func(name, party, org, level string) map[string]any {
		u := map[string]any{"username": name, "party": party, "password": name + "-initial-pass"}
		if org != "" {
			u["access"] = map[string]any{"organization": org, "level": level}
		}
		return u
	}
	h.must(h.do("POST", users(""), root, newUser("marta", martaP.ID, acme.ID, "Full"), &marta), 201, "marta")
	if !marta.MustChangePassword || !marta.HasPassword || len(marta.Accesses) != 1 || marta.Accesses[0].Level != "Full" {
		t.Fatalf("marta: %+v", marta)
	}
	h.must(h.do("POST", users(""), root, newUser("MARTA", carlosP.ID, acme.ID, "Full"), nil), 422, "user names are unique ignoring case")
	h.must(h.do("POST", users(""), root, newUser("ghost", fw.NewUUID().String(), acme.ID, "Full"), nil), 400, "unknown party")
	h.must(h.do("POST", users(""), root, newUser("carlos", carlosP.ID, martaP.ID, "Full"), nil), 400, "accesses go to internal organizations")
	h.must(h.do("POST", users(""), root, newUser("carlos", carlosP.ID, acme.ID, "Owner"), nil), 400, "the level is not free text")
	weak := newUser("carlos", carlosP.ID, acme.ID, "Full")
	weak["password"] = "short"
	h.must(h.do("POST", users(""), root, weak, nil), 400, "password policy")
	h.must(h.do("PUT", users("/"+marta.ID+"/roles/"+sdomain.RoleOrganizationAdmin.String()), root, nil, &marta), 200, "marta is organization administrator")
	if len(marta.Roles) != 1 || marta.Roles[0].Name != "OrganizationAdmin" {
		t.Fatalf("roles of marta: %+v", marta.Roles)
	}
	h.must(h.do("POST", users(""), root, newUser("pedro", pedroP.ID, beta.ID, "Full"), &pedro), 201, "pedro")
	h.must(h.do("PUT", users("/"+pedro.ID+"/roles/"+sdomain.RoleStandardUser.String()), root, nil, nil), 200, "pedro is a standard user")

	// --- Delegated administration: Marta administers Acme -------------------------------------
	martaTok := h.signIn("marta", "marta-initial-pass", "marta-password-01")
	m := bearer(martaTok)
	mc := h.context(m)
	if mc.GlobalAdmin || !mc.HasPermission(papp.PermPartyCreate) || !mc.HasPermission(sapp.PermUserUpdate) ||
		mc.HasPermission(sapp.PermRoleUpdate) || !mc.CanWrite(fw.MustParseUUID(acme.ID)) || mc.CanRead(fw.MustParseUUID(beta.ID)) {
		t.Fatalf("context of marta: %+v", mc)
	}
	var seen fw.Page[papp.PartyDTO]
	h.must(h.do("GET", "/api/parties", m, nil, &seen), 200, "marta reads parties in her scope")

	h.must(h.do("POST", users(""), m, newUser("carlos", carlosP.ID, "", ""), nil), 400, "a new user needs a first access")
	h.must(h.do("POST", users(""), m, newUser("carlos", carlosP.ID, beta.ID, "Full"), nil), 403, "marta does not grant beta")
	h.must(h.do("POST", users(""), m, newUser("carlos", carlosP.ID, acme.ID, "ReadOnly"), &carlos), 201, "marta registers carlos")
	h.must(h.do("PUT", users("/"+carlos.ID+"/roles/"+sdomain.RoleStandardUser.String()), m, nil, nil), 200, "a role within her permissions")
	h.must(h.do("PUT", users("/"+carlos.ID+"/roles/"+sdomain.RoleGlobalSuperAdmin.String()), m, nil, nil), 403, "nobody grants what they do not have")
	h.must(h.do("PUT", users("/"+marta.ID+"/roles/"+sdomain.RoleStandardUser.String()), m, nil, nil), 403, "nobody administers themselves")
	h.must(h.do("PUT", users("/"+marta.ID+"/organizations/"+acme.ID), m, map[string]any{"level": "Full"}, nil), 403, "nor grants themselves organizations")
	h.must(h.do("POST", users("/"+marta.ID+"/deactivate"), m, nil, nil), 403, "nor deactivates themselves")
	h.must(h.do("POST", "/api/security/roles", m, map[string]any{"name": "Mine", "permissions": []string{}}, nil), 403, "roles belong to the global administrator")

	// Pedro works in Beta: he does not exist for Marta until she gives him access to Acme, and
	// even then she cannot administer him (in C# she could then edit or delete him).
	h.must(h.do("GET", users("/"+pedro.ID), m, nil, nil), 404, "out of sight")
	h.must(h.do("POST", users("/"+pedro.ID+"/deactivate"), m, nil, nil), 404, "out of sight, uniform 404")
	h.must(h.do("PUT", users("/"+pedro.ID+"/organizations/"+beta.ID), m, map[string]any{"level": "ReadOnly"}, nil), 403, "beta is not hers to grant")
	h.must(h.do("PUT", users("/"+pedro.ID+"/organizations/"+acme.ID), m, map[string]any{"level": "ReadOnly"}, &pedro), 200, "acme is")
	if len(pedro.Accesses) != 2 {
		t.Fatalf("accesses of pedro: %+v", pedro.Accesses)
	}
	h.must(h.do("GET", users("/"+pedro.ID), m, nil, nil), 200, "now visible")
	h.must(h.do("POST", users("/"+pedro.ID+"/deactivate"), m, nil, nil), 403, "but she does not encompass beta")
	h.must(h.do("DELETE", users("/"+pedro.ID+"/roles/"+sdomain.RoleStandardUser.String()), m, nil, nil), 403, "nor touches his roles")
	h.must(h.do("POST", users("/"+pedro.ID+"/reset-password"), m, map[string]any{"password": "taken-over-password"}, nil), 403, "nor his password")
	var page fw.Page[sapp.UserDTO]
	h.must(h.do("GET", users("?administrable=true"), m, nil, &page), 200, "administrable users")
	if names := userNames(page); !slices.Equal(names, []string{"carlos", "marta"}) {
		t.Fatalf("marta administers: %v", names)
	}
	h.must(h.do("GET", users(""), m, nil, &page), 200, "visible users")
	if names := userNames(page); !slices.Equal(names, []string{"carlos", "marta", "pedro"}) {
		t.Fatalf("marta sees: %v", names)
	}
	h.must(h.do("DELETE", users("/"+pedro.ID+"/organizations/"+acme.ID), m, nil, nil), 200, "she takes acme back")
	h.must(h.do("GET", users("/"+pedro.ID), m, nil, nil), 404, "and he is out of sight again")

	// --- Revocation takes effect on the next request with the same token (D7) ----------------
	carlosTok := h.signIn("carlos", "carlos-initial-pass", "carlos-password-01")
	c := bearer(carlosTok)
	before := h.context(c).PolicyVersion
	h.must(h.do("GET", "/api/parties", c, nil, nil), 200, "a standard user reads parties")
	h.must(h.do("POST", "/api/persons", c, map[string]any{"givenName": "X", "firstSurname": "Y", "affiliation": map[string]any{
		"organization": acme.ID, "relationshipType": pdomain.RelEmployment.String()}}, nil), 403, "read-only access does not write")
	h.must(h.do("GET", users(""), c, nil, nil), 403, "a standard user has no Security permissions")
	h.must(h.do("DELETE", users("/"+carlos.ID+"/roles/"+sdomain.RoleStandardUser.String()), root, nil, nil), 200, "revoke the role")
	h.must(h.do("GET", "/api/parties", c, nil, nil), 403, "the same token no longer reads")
	if h.context(c).PolicyVersion == before {
		t.Fatal("the policy version changes with the revocation")
	}
	h.must(h.do("PUT", users("/"+carlos.ID+"/roles/"+sdomain.RoleStandardUser.String()), root, nil, nil), 200, "assign it again")
	h.must(h.do("POST", users("/"+carlos.ID+"/deactivate"), root, nil, &carlos), 200, "deactivate")
	h.must(h.do("GET", "/api/parties", c, nil, nil), 403, "a deactivated user is denied at once")
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": carlosTok.RefreshToken}, nil), 401, "its sessions ended")
	if _, status := h.login("carlos", "carlos-password-01"); status != 401 {
		t.Fatalf("a deactivated user cannot sign in: %d", status)
	}
	h.must(h.do("POST", users("/"+carlos.ID+"/activate"), root, nil, nil), 200, "reactivate")

	// --- Lockout -------------------------------------------------------------------------------
	for range 3 {
		if _, status := h.login("carlos", "wrong-password-000"); status != 401 {
			t.Fatalf("wrong password: %d", status)
		}
	}
	if _, status := h.login("carlos", "carlos-password-01"); status != 401 {
		t.Fatalf("locked after three failures, even with the right password: %d", status)
	}
	if _, status := h.login("nobody", "whatever-password"); status != 401 {
		t.Fatalf("an unknown user gets the same answer: %d", status)
	}
	h.must(h.do("GET", users("/"+carlos.ID), root, nil, &carlos), 200, "carlos")
	if !carlos.Locked || carlos.LockedUntil == nil {
		t.Fatalf("locked: %+v", carlos)
	}
	h.must(h.do("POST", users("/"+carlos.ID+"/unlock"), m, nil, &carlos), 200, "marta unlocks her user")
	if carlos.Locked {
		t.Fatal("unlocked")
	}

	// --- Sessions: rotation, reuse detection, logout -------------------------------------------
	first, status := h.login("carlos", "carlos-password-01")
	h.must(status, 200, "login after unlock")
	var second sapp.Tokens
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": first.RefreshToken}, &second), 200, "refresh")
	if second.RefreshToken == first.RefreshToken || second.AccessToken == "" {
		t.Fatal("a refresh rotates the token")
	}
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": first.RefreshToken}, nil), 401, "a rotated token presented again")
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": second.RefreshToken}, nil), 401, "ends the whole family")
	third, _ := h.login("carlos", "carlos-password-01")
	h.must(h.do("POST", "/api/auth/logout", "", map[string]any{"refreshToken": third.RefreshToken}, nil), 204, "logout")
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": third.RefreshToken}, nil), 401, "after logout")
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": "never-issued"}, nil), 401, "unknown token")
	fourth, _ := h.login("carlos", "carlos-password-01")
	h.must(h.do("POST", users("/"+carlos.ID+"/reset-password"), m, map[string]any{"password": "carlos-reset-pass-1"}, nil), 200, "marta resets his password")
	h.must(h.do("POST", "/api/auth/refresh", "", map[string]any{"refreshToken": fourth.RefreshToken}, nil), 401, "a reset ends the sessions")
	h.signIn("carlos", "carlos-reset-pass-1", "carlos-password-02")

	// --- External identity: issuer + subject linked with a user (decision 1) ------------------
	ext := "Bearer ext:" + idp + "|sub-anna"
	h.must(h.do("GET", "/api/auth/context", ext, nil, nil), 401, "a valid token of an identity nobody linked does not get in")
	h.must(h.do("POST", users(""), root, map[string]any{"username": "anna", "party": annaP.ID,
		"access": map[string]any{"organization": beta.ID, "level": "Full"}}, &anna), 201, "a user without password")
	h.must(h.do("PUT", users("/"+anna.ID+"/roles/"+sdomain.RoleStandardUser.String()), root, nil, nil), 200, "role of anna")
	h.must(h.do("POST", users("/"+anna.ID+"/identities"), root, map[string]any{"issuer": idp, "subject": "sub-anna"}, &anna), 200, "link")
	h.must(h.do("POST", users("/"+pedro.ID+"/identities"), root, map[string]any{"issuer": idp, "subject": "sub-anna"}, nil), 422, "an identity belongs to one user")
	if anna.HasPassword || len(anna.Identities) != 1 {
		t.Fatalf("anna: %+v", anna)
	}
	ac := h.context(ext)
	if ac.Subject.String() != anna.ID || ac.SubjectName != "anna" || ac.ActorPartyID.String() != annaP.ID ||
		!ac.HasPermission(papp.PermPartyRead) || !ac.CanWrite(fw.MustParseUUID(beta.ID)) {
		t.Fatalf("the external identity resolves to the same user, roles and accesses: %+v", ac)
	}
	h.must(h.do("GET", "/api/parties", ext, nil, nil), 200, "and uses the other contexts like any user")
	if _, status := h.login("anna", "any-password-at-all"); status != 401 {
		t.Fatalf("a user without password never signs in with one: %d", status)
	}
	h.must(h.do("POST", "/api/auth/change-password", ext, map[string]any{"currentPassword": "x", "newPassword": "anna-password-0001"}, nil), 422, "nothing to change")
	h.must(h.do("DELETE", users("/"+anna.ID+"/identities"), root, map[string]any{"issuer": idp, "subject": "sub-anna"}, nil), 200, "unlink")
	h.must(h.do("GET", "/api/auth/context", ext, nil, nil), 401, "unlinked")

	// --- Roles and the catalog -----------------------------------------------------------------
	var catalog []sapp.PermissionDTO
	h.must(h.do("GET", "/api/security/permissions", root, nil, &catalog), 200, "catalog")
	if len(catalog) != len(sdomain.OwnPermissions())+len(papp.Permissions()) {
		t.Fatalf("catalog: %d entries", len(catalog))
	}
	var clerk sapp.RoleDTO
	h.must(h.do("POST", "/api/security/roles", root, map[string]any{"name": "Purchasing clerk", "permissions": []string{"Nowhere.Thing.Read"}}, nil), 400, "unknown permission")
	h.must(h.do("POST", "/api/security/roles", root, map[string]any{"name": "Super", "permissions": []string{"*.*.*"}}, nil), 400, "no custom role carries the wildcard")
	h.must(h.do("POST", "/api/security/roles", root, map[string]any{"name": "Purchasing clerk", "permissions": []string{"Parties.Party.Read"}}, &clerk), 201, "define")
	h.must(h.do("POST", "/api/security/roles", root, map[string]any{"name": "purchasing CLERK", "permissions": []string{}}, nil), 422, "role names are unique")
	h.must(h.do("PUT", "/api/security/roles/"+clerk.ID+"/permissions", root, map[string]any{"permissions": []string{"Parties.Party.Read", "Parties.Relationship.Read"}}, &clerk), 200, "set permissions")
	h.must(h.do("PUT", "/api/security/roles/"+sdomain.RoleStandardUser.String(), root, map[string]any{"name": "Renamed"}, nil), 422, "system roles are not edited")
	h.must(h.do("DELETE", "/api/security/roles/"+sdomain.RoleCustomer.String(), root, nil, nil), 422, "nor retired")
	h.must(h.do("PUT", users("/"+carlos.ID+"/roles/"+clerk.ID), m, nil, nil), 200, "marta assigns a custom role within her permissions")
	h.must(h.do("DELETE", "/api/security/roles/"+clerk.ID, root, nil, nil), 422, "a role in use is not retired")

	// A context that stops declaring its permissions: they stop granting at once (fail closed),
	// the system roles follow the rule and the custom role is left as the administrator made it.
	if report, err := h.sec.SyncCatalog(ctx); err != nil || report.Deactivated != len(papp.Permissions()) || report.RolesUpdated == 0 {
		t.Fatalf("sync without parties: %+v %v", report, err)
	}
	h.must(h.do("GET", "/api/parties", m, nil, nil), 403, "undeclared permissions grant nothing")
	if report, err := h.sec.SyncCatalog(ctx, papp.Permissions()...); err != nil || report.Reactivated != len(papp.Permissions()) {
		t.Fatalf("sync with parties again: %+v %v", report, err)
	}
	h.must(h.do("GET", "/api/parties", m, nil, nil), 200, "declared again")
	h.must(h.do("GET", "/api/security/roles/"+clerk.ID, root, nil, &clerk), 200, "custom role")
	if !slices.Equal(clerk.Permissions, []string{"Parties.Party.Read", "Parties.Relationship.Read"}) {
		t.Fatalf("the synchronization never touches a custom role: %v", clerk.Permissions)
	}
	h.must(h.do("DELETE", users("/"+carlos.ID+"/roles/"+clerk.ID), m, nil, nil), 200, "revoke the custom role")
	h.must(h.do("DELETE", "/api/security/roles/"+clerk.ID, root, nil, nil), 204, "retire")
	var roles []sapp.RoleDTO
	h.must(h.do("GET", "/api/security/roles", root, nil, &roles), 200, "roles")
	if len(roles) != 5 || !roles[0].System {
		t.Fatalf("the five system roles remain: %+v", roles)
	}

	// --- An active global administrator always remains -----------------------------------------
	h.must(h.do("POST", users("/"+rootTok.UserID+"/deactivate"), root, nil, nil), 403, "root does not deactivate itself")
	h.must(h.do("PUT", users("/"+rootTok.UserID+"/organizations/"+acme.ID), root, map[string]any{"level": "Full"}, nil), 403, "nor grants itself organizations")
	// Give the only global administrator an access to Acme through a second, temporary one, so
	// Marta encompasses it: she may deactivate users of Acme, but not the last global administrator.
	var root2 sapp.UserDTO
	h.must(h.do("POST", users(""), root, map[string]any{"username": "root2", "party": martaP.ID, "password": "root2-initial-pass"}, &root2), 201, "second administrator")
	h.must(h.do("PUT", users("/"+root2.ID+"/roles/"+sdomain.RoleGlobalSuperAdmin.String()), root, nil, nil), 200, "global")
	r2 := bearer(h.signIn("root2", "root2-initial-pass", "root2-password-001"))
	h.must(h.do("PUT", users("/"+rootTok.UserID+"/organizations/"+acme.ID), r2, map[string]any{"level": "Full"}, nil), 200, "root gets an access to acme")
	h.must(h.do("POST", users("/"+root2.ID+"/deactivate"), root, nil, nil), 200, "root2 leaves: root remains")
	h.must(h.do("POST", users("/"+rootTok.UserID+"/deactivate"), m, nil, nil), 422, "the last global administrator stays")
	h.must(h.do("DELETE", users("/"+rootTok.UserID+"/roles/"+sdomain.RoleGlobalSuperAdmin.String()), m, nil, nil), 403, "and its role is out of marta's reach")
	h.must(h.do("DELETE", users("/"+rootTok.UserID+"/organizations/"+acme.ID), m, nil, nil), 200, "marta takes acme back from root")

	// --- Directory for other contexts, audit and Published Language ---------------------------
	refs, err := h.sec.Users.Resolve(ctx, []string{marta.ID, fw.NewUUID().String(), "not-an-id"})
	if err != nil || len(refs) != 1 || refs[marta.ID].Username != "marta" || refs[marta.ID].PartyID != martaP.ID {
		t.Fatalf("resolve: %+v %v", refs, err)
	}
	byParty, err := h.sec.Users.ByParty(ctx, []string{martaP.ID})
	if err != nil || len(byParty[martaP.ID]) != 2 { // marta and root2 are the same person
		t.Fatalf("by party: %+v %v", byParty, err)
	}
	trail, err := h.sec.Audit.Trail(ctx, sdomain.UserKind, carlos.ID)
	if err != nil || len(trail) < 10 || trail[0].Actor.Name != "marta" || trail[0].Operation != application.AuditCreated {
		t.Fatalf("audit of carlos: %d records %v", len(trail), err)
	}
	for _, rec := range trail {
		for _, ch := range rec.Changes {
			if strings.Contains(strings.ToLower(ch.Field), "hash") {
				t.Fatalf("the audit never records the hash: %+v", ch)
			}
		}
	}
	broker := inprocess.NewBroker()
	store := memory.NewStore("siem")
	siem := messaging.NewConsumer("siem", memory.NewInbox(store), store)
	published := map[string]int{}
	count := func(name string) { published[name]++ }
	messaging.Handle(siem, func(_ context.Context, e contracts.UserRegisteredV1, _ application.Envelope) error {
		count("registered")
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.UserLockedV1, _ application.Envelope) error {
		count("locked")
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.UserDeactivatedV1, _ application.Envelope) error {
		count("deactivated")
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.UserPasswordChangedV1, _ application.Envelope) error {
		if e.Reset {
			count("reset")
		}
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.OrganizationAccessGrantedV1, _ application.Envelope) error {
		count("granted")
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.OrganizationAccessRevokedV1, _ application.Envelope) error {
		count("revoked")
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.IdentityLinkedV1, _ application.Envelope) error {
		count("linked")
		return nil
	})
	messaging.Handle(siem, func(_ context.Context, e contracts.RoleRetiredV1, _ application.Envelope) error {
		count("retired")
		return nil
	})
	broker.Subscribe("siem", siem)
	for {
		n, err := h.sec.Relay(broker).RelayOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	// Six users registered; a password set by an administrator counts as a reset: four on
	// registration (anna has none, root set its own) plus the reset of carlos.
	want := map[string]int{"registered": 6, "locked": 1, "deactivated": 2, "reset": 5, "granted": 6, "revoked": 2, "linked": 1, "retired": 1}
	for name, n := range want {
		if published[name] != n {
			t.Errorf("published %s: %d, want %d (all: %v)", name, published[name], n, published)
		}
	}
}

func userNames(p fw.Page[sapp.UserDTO]) []string {
	names := make([]string, len(p.Items))
	for i, u := range p.Items {
		names[i] = u.Username
	}
	return names
}

func TestSecurity_WithParties_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	// In memory nothing is seeded: the first synchronization declares everything.
	h.start(len(sdomain.OwnPermissions()) + len(papp.Permissions()))
	h.scenario("mem")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), sinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	// The migration seeded the own permissions and the system roles: only Parties is new.
	h.start(len(papp.Permissions()))
	h.scenario("sql")
}

func TestSecurity_SQLiteSeedMatchesTheCSharpIdentities(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw)
	m, err := sinfra.Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	roles, err := sqlrepo.MustRepository(db, sinfra.RoleMapping()).Find(ctx, nil, sdomain.RoleFieldName.Asc())
	if err != nil || len(roles) != 5 {
		t.Fatalf("roles: %d %v", len(roles), err)
	}
	byName := map[string]*sdomain.Role{}
	for _, r := range roles {
		byName[r.Name()] = r
	}
	admin, org := byName["GlobalSuperAdmin"], byName["OrganizationAdmin"]
	if admin.ID() != sdomain.RoleGlobalSuperAdmin || !admin.IsSystem() || !slices.Equal(admin.Permissions(), []sdomain.Permission{sdomain.Wildcard}) {
		t.Fatalf("GlobalSuperAdmin: %s %v", admin.ID(), admin.Permissions())
	}
	if org.ID().String() != "20000000-0000-0000-0001-000000000002" || slices.Contains(org.Permissions(), sdomain.PermRoleUpdate) ||
		!slices.Contains(org.Permissions(), sdomain.PermUserCreate) || len(byName["Customer"].Permissions()) != 0 {
		t.Fatalf("OrganizationAdmin: %s %v", org.ID(), org.Permissions())
	}
	perms, err := sqlrepo.MustRepository(db, sinfra.PermissionMapping()).Find(ctx, nil)
	if err != nil || len(perms) != len(sdomain.OwnPermissions()) {
		t.Fatalf("permissions: %d %v", len(perms), err)
	}
	sinfra.DropAll(ctx, db)
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatalf("migrate again after dropping: %v", err)
	}
}

func TestPasswordHasher(t *testing.T) {
	h := sinfra.NewPBKDF2(2000)
	hash, err := h.Hash("correct horse battery")
	if err != nil || !strings.HasPrefix(hash, "pbkdf2-sha256$2000$") {
		t.Fatalf("self-describing hash: %q %v", hash, err)
	}
	other, _ := h.Hash("correct horse battery")
	if other == hash {
		t.Fatal("every hash has its own salt")
	}
	if ok, stale := h.Verify("correct horse battery", hash); !ok || stale {
		t.Fatalf("verify: %v %v", ok, stale)
	}
	if ok, _ := h.Verify("correct horse batterx", hash); ok {
		t.Fatal("a wrong password does not verify")
	}
	// Raising the cost needs no migration: old hashes verify and are reported as stale.
	if ok, stale := sinfra.NewPBKDF2(4000).Verify("correct horse battery", hash); !ok || !stale {
		t.Fatalf("stale: %v %v", ok, stale)
	}
	for _, bad := range []string{"", "plain", "pbkdf2-sha256$0$AAAA$AAAA", "pbkdf2-sha256$99999999999$AAAA$AAAA", "argon2id$1$AAAA$AAAA", "pbkdf2-sha256$10$!$!"} {
		if ok, _ := h.Verify("x", bad); ok {
			t.Errorf("%q must not verify", bad)
		}
	}
	if sinfra.DefaultIterations != 600_000 {
		t.Fatal("the default cost is the OWASP figure for PBKDF2-HMAC-SHA256")
	}
	// A C# row (PBKDF2_SHA256, 10 000 iterations, base64 hash, raw salt) keeps its password.
	salt := []byte("0123456789abcdef")
	key, err := pbkdf2.Key(sha256.New, "legacy password 1", salt, 10_000, 32) // what Pbkdf2PasswordHasher.cs computes
	if err != nil {
		t.Fatal(err)
	}
	imported, err := sinfra.FromCSharp(base64.StdEncoding.EncodeToString(key), salt)
	if err != nil {
		t.Fatal(err)
	}
	if ok, stale := sinfra.NewPBKDF2(0).Verify("legacy password 1", imported); !ok || !stale {
		t.Fatalf("an imported hash verifies and is upgraded on the next login: %v %v", ok, stale)
	}
	if _, err := sinfra.FromCSharp("not base64!", salt); err == nil {
		t.Fatal("a malformed C# hash is rejected")
	}
}

// Every bounded context declares its permissions and the host passes them to the catalog: the 149
// codes the twenty-five business contexts check, plus the eight of Security and the wildcard.
func TestCatalog_EveryContextDeclaresItsPermissions(t *testing.T) {
	declared := map[string][]authz.Permission{
		"Accounting": accapp.Permissions(), "Assets": astapp.Permissions(), "Billing": bilapp.Permissions(), "Facilities": facapp.Permissions(),
		"Fiscal": fisapp.Permissions(), "Geography": geoapp.Permissions(), "HR": hrapp.Permissions(), "Inventory": invapp.Permissions(),
		"Orders": ordapp.Permissions(), "Parties": papp.Permissions(), "Products": prdapp.Permissions(),
		"Payments": payapp.Permissions(), "Payroll": prlapp.Permissions(), "Purchases": purapp.Permissions(),
		"Receivables": recapp.Permissions(), "Treasury": treapp.Permissions(), "Financial": finapp.Permissions(), "Exports": expapp.Permissions(), "Imports": impapp.Permissions(), "Exchange": exgapp.Permissions(), "Audit": autapp.Permissions(), "Modules": modapp.Permissions(), "Shipments": shpapp.Permissions(), "Documents": docapp.Permissions(), "Work": wrkapp.Permissions(), sdomain.Namespace: sapp.Permissions(),
	}
	var all []authz.Permission
	for namespace, perms := range declared {
		for _, p := range perms {
			if !p.WellFormed() || sdomain.Permission(p).Namespace() != namespace || slices.Contains(all, p) {
				t.Errorf("%s declares %q: malformed, foreign or duplicated", namespace, p)
			}
			all = append(all, p)
		}
	}
	if len(all) != 149+len(sapp.Permissions()) || len(sapp.Permissions()) != 8 {
		t.Fatalf("declared permissions: %d", len(all))
	}

	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("catalog"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	sec := security.Compose(sw, security.WithPasswordHasher(sinfra.NewPBKDF2(1000)))
	report, err := sec.SyncCatalog(ctx, all...)
	if err != nil || report.Added != len(all)+1 { // plus the wildcard
		t.Fatalf("sync: %+v %v", report, err)
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service})
	ac.GlobalAdmin = true
	roles, err := sec.Service.ListRoles.Handle(authz.WithContext(ctx, ac), sapp.ListRoles{})
	if err != nil {
		t.Fatal(err)
	}
	granted := map[string][]string{}
	for _, r := range roles {
		granted[r.Name] = r.Permissions
	}
	// The standard rule over the real catalog: actions beyond Read, Create and Update (issuing an
	// invoice, approving a payslip, submitting a tax filing, settling a remittance...) reach the
	// organization administrator or a custom role, never the standard user.
	std := granted["StandardUser"]
	for _, special := range []string{"Billing.Invoice.Issue", "Payroll.Payslip.Approve", "Fiscal.Filing.Submit", "Treasury.Remittance.Settle",
		"Accounting.Entry.Reverse", "Parties.PartyRole.Assign", "Parties.Relationship.SetTrial", "Purchases.Invoice.Register", "Orders.Order.Confirm",
		"Inventory.Stock.Adjust", "Security.User.Read"} {
		if slices.Contains(std, special) || (special != "Security.User.Read" && !slices.Contains(granted["OrganizationAdmin"], special)) {
			t.Errorf("%s: standard user %v, organization admin %v", special, slices.Contains(std, special), slices.Contains(granted["OrganizationAdmin"], special))
		}
	}
	if len(granted["OrganizationAdmin"]) != len(all)-1 || slices.Contains(granted["OrganizationAdmin"], string(sapp.PermRoleUpdate)) {
		t.Errorf("OrganizationAdmin holds everything but the roles: %d of %d", len(granted["OrganizationAdmin"]), len(all))
	}
	if len(granted["ReadOnlyUser"]) != 60+3 || !slices.Contains(std, "Billing.Invoice.Create") || len(granted["Customer"]) != 0 ||
		!slices.Equal(granted["GlobalSuperAdmin"], []string{"*.*.*"}) {
		t.Errorf("read only %d, customer %d, global %v", len(granted["ReadOnlyUser"]), len(granted["Customer"]), granted["GlobalSuperAdmin"])
	}
}

func TestSecurity_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/security/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
	// Security is not tied to the shared-secret JWT: only the HTTP adapter knows it.
	for _, layer := range []string{"domain", "contracts", "application", "infrastructure"} {
		archtest.AssertTreeDoesNotImport(t, layer, []string{"/pkg/distribution/jwtauth"})
	}
}
