package distribution_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// TestAuthorize_EndToEnd wires JWT authentication, the generic resolver, the HTTP middleware and
// a use case protected by pipeline.RequirePermission, and checks every outcome of contract v1.
func TestAuthorize_EndToEnd(t *testing.T) {
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("test-secret")})
	dir := authorization.NewMemoryDirectory()
	orgA, orgB := domain.NewUUID(), domain.NewUUID()
	ana, anaParty, luis, ghost := domain.NewUUID(), domain.NewUUID(), domain.NewUUID(), domain.NewUUID()
	dir.Put(ana, authz.Subject{Active: true, Permissions: []authz.Permission{"Parties.Party.Create"},
		Grants: []authz.Grant{{OrganizationID: orgA, Level: authz.Full}, {OrganizationID: orgB, Level: authz.ReadOnly}}})
	dir.Put(luis, authz.Subject{Active: true, Permissions: []authz.Permission{"Parties.Party.Read"}})
	resolver := authorization.NewResolver(dir, authorization.Options{})

	type cmd struct{ Org domain.UUID }
	var seenActor string
	create := pipeline.RequirePermission[cmd, string]("Parties.Party.Create")(
		application.HandlerFunc[cmd, string](func(ctx context.Context, c cmd) (string, error) {
			if err := authz.RequireWrite(ctx, c.Org); err != nil {
				return "", err
			}
			seenActor = application.ActorFrom(ctx).Name
			return "created", nil
		}))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /parties", func(w http.ResponseWriter, r *http.Request) {
		org, _ := domain.ParseUUID(r.URL.Query().Get("org"))
		res, err := create.Handle(r.Context(), cmd{Org: org})
		distribution.Respond(w, r, res, err, http.StatusCreated)
	})
	srv := httptest.NewServer(distribution.Chain(mux, distribution.Correlation(), distribution.Authorize(jwt, resolver)))
	defer srv.Close()

	token := func(sub, party domain.UUID, name string) string {
		c := jwtauth.Claims{Subject: sub.String(), Username: name, ExpiresAt: domain.Now().Add(time.Hour).Unix()}
		if !party.IsZero() {
			c.PartyID = party.String()
		}
		s, _ := jwt.Issue(c)
		return "Bearer " + s
	}
	call := func(auth, scope string, org domain.UUID) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+"/parties?org="+org.String(), nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if scope != "" {
			req.Header.Set(distribution.OrganizationScopeHeader, scope)
		}
		// A client-supplied actor header must be ignored by Authorize.
		req.Header.Set(distribution.ActorNameHeader, "Mallory")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}

	cases := []struct {
		name   string
		auth   string
		scope  string
		org    domain.UUID
		status int
	}{
		{"no token", "", "", orgA, http.StatusUnauthorized},
		{"bad token", "Bearer x.y.z", "", orgA, http.StatusUnauthorized},
		{"incomplete principal", token(ana, domain.UUID{}, "ana"), "", orgA, http.StatusUnauthorized},
		{"unknown subject", token(ghost, anaParty, "ghost"), "", orgA, http.StatusForbidden},
		{"invalid scope header", token(ana, anaParty, "ana"), "not-a-uuid", orgA, http.StatusBadRequest},
		{"missing permission", token(luis, domain.NewUUID(), "luis"), "", orgA, http.StatusForbidden},
		{"read-only organization", token(ana, anaParty, "ana"), "", orgB, http.StatusForbidden},
		{"full grant outside requested scope", token(ana, anaParty, "ana"), orgB.String(), orgA, http.StatusForbidden},
		{"allowed", token(ana, anaParty, "ana"), orgA.String() + ", " + orgB.String(), orgA, http.StatusCreated},
	}
	for _, tc := range cases {
		res := call(tc.auth, tc.scope, tc.org)
		if res.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, res.StatusCode, tc.status)
		}
		if tc.status == http.StatusUnauthorized && res.Header.Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("%s: missing WWW-Authenticate", tc.name)
		}
	}
	if seenActor != "ana" {
		t.Fatalf("the actor must come from the resolved context, got %q", seenActor)
	}

	dir.FailWith(errors.New("security database down"))
	res := call(token(ana, anaParty, "ana"), "", orgA)
	if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") != "5" {
		t.Fatalf("indeterminate must be 503 + Retry-After: %d %q", res.StatusCode, res.Header.Get("Retry-After"))
	}
}

func TestRequirePermission_HTTP(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := distribution.RequirePermission("Ops.Cache.Flush", ok)
	c, _ := authz.NewContext(authz.Context{Subject: domain.NewUUID(), SubjectName: "svc", Kind: authz.Service,
		Permissions: []authz.Permission{"Ops.Cache.Flush"}})
	for _, tc := range []struct {
		ctx    context.Context
		status int
	}{{context.Background(), http.StatusUnauthorized}, {authz.WithContext(context.Background(), c), http.StatusNoContent}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil).WithContext(tc.ctx))
		if rec.Code != tc.status {
			t.Errorf("status %d, want %d", rec.Code, tc.status)
		}
	}
}
