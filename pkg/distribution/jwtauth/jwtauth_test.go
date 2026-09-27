package jwtauth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestHS256(t *testing.T) {
	if _, err := jwtauth.New(jwtauth.Config{}); !errors.Is(err, jwtauth.ErrMissingSecret) {
		t.Fatal("a secret is mandatory")
	}
	h, _ := jwtauth.New(jwtauth.Config{Secret: []byte("s3cr3t")})
	other, _ := jwtauth.New(jwtauth.Config{Secret: []byte("other")})
	otherAud, _ := jwtauth.New(jwtauth.Config{Secret: []byte("s3cr3t"), Audience: "else"})
	sub, party := domain.NewUUID(), domain.NewUUID()
	exp := domain.Now().Add(time.Hour).Unix()
	ctx := context.Background()

	tok, err := h.Issue(jwtauth.Claims{Subject: sub.String(), Username: "ana", PartyID: party.String(), ExpiresAt: exp})
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Authenticate(ctx, "Bearer "+tok)
	if err != nil || p.Subject != sub || p.PartyID != party || p.Name != "ana" || p.Kind != authz.Human || !p.Complete() {
		t.Fatalf("valid token: %+v %v", p, err)
	}
	svc, _ := h.Issue(jwtauth.Claims{Subject: sub.String(), Username: "billing", ActorKind: "service", ExpiresAt: exp})
	if p, _ := h.Authenticate(ctx, svc); p.Kind != authz.Service {
		t.Fatal("actorKind=service")
	}

	expired, _ := h.Issue(jwtauth.Claims{Subject: sub.String(), Username: "ana", ExpiresAt: domain.Now().Add(-2 * time.Minute).Unix()})
	skewed, _ := h.Issue(jwtauth.Claims{Subject: sub.String(), Username: "ana", ExpiresAt: domain.Now().Add(-30 * time.Second).Unix()})
	noExp, _ := h.Issue(jwtauth.Claims{Subject: sub.String(), Username: "ana"})
	forged, _ := other.Issue(jwtauth.Claims{Subject: sub.String(), Username: "ana", ExpiresAt: exp})
	wrongAud, _ := otherAud.Issue(jwtauth.Claims{Subject: sub.String(), Username: "ana", ExpiresAt: exp})
	parts := strings.Split(tok, ".")
	none := "eyJhbGciOiJub25lIn0." + parts[1] + "."

	if _, err := h.Authenticate(ctx, skewed); err != nil {
		t.Fatalf("one minute of clock skew is tolerated: %v", err)
	}
	for name, bad := range map[string]string{"expired": expired, "no exp": noExp, "forged": forged,
		"audience": wrongAud, "alg none": none, "garbage": "a.b.c", "tampered": parts[0] + "." + parts[1] + "x." + parts[2]} {
		if _, err := h.Authenticate(ctx, bad); !errors.Is(err, authz.ErrInvalidCredentials) || !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := h.Authenticate(ctx, "  "); !errors.Is(err, authz.ErrNoCredentials) {
		t.Fatal("empty credentials")
	}
}
