// Package jwtauth implements authz.Authenticator for HS256 bearer tokens with the standard
// library only (Go port of AddFwJwtAuthentication): issuer, audience, lifetime with clock skew
// and signature are validated; claims sub, username, partyId and actorKind become the Principal.
package jwtauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Defaults of the Karpo security service.
const (
	DefaultIssuer   = "erp-security"
	DefaultAudience = "erp-api"
	DefaultSkew     = time.Minute
)

// Config configures the authenticator. Secret is mandatory: without it there is no valid
// authentication (D-SECRETS-IN-APPSETTINGS).
type Config struct {
	Secret   []byte
	Issuer   string
	Audience string
	Skew     time.Duration
}

// HS256 validates and issues HS256 JWTs.
type HS256 struct{ cfg Config }

var _ authz.Authenticator = (*HS256)(nil)

// ErrMissingSecret is returned by New without a secret.
var ErrMissingSecret = errors.New("jwtauth: secret not configured")

// New builds an HS256 authenticator, applying the defaults.
func New(cfg Config) (*HS256, error) {
	if len(cfg.Secret) == 0 {
		return nil, ErrMissingSecret
	}
	if cfg.Issuer == "" {
		cfg.Issuer = DefaultIssuer
	}
	if cfg.Audience == "" {
		cfg.Audience = DefaultAudience
	}
	if cfg.Skew == 0 {
		cfg.Skew = DefaultSkew
	}
	return &HS256{cfg: cfg}, nil
}

// Claims are the claims Karpo reads and writes.
type Claims struct {
	Subject   string   `json:"sub"`
	Username  string   `json:"username"`
	PartyID   string   `json:"partyId,omitempty"`
	ActorKind string   `json:"actorKind,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	Issuer    string   `json:"iss"`
	Audience  Audience `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	NotBefore int64    `json:"nbf,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
}

// Audience accepts the "aud" claim as a string or an array.
type Audience []string

// UnmarshalJSON implements json.Unmarshaler.
func (a *Audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = Audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

// MarshalJSON implements json.Marshaler.
func (a Audience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}
	return json.Marshal([]string(a))
}

var b64 = base64.RawURLEncoding

// Authenticate implements authz.Authenticator. credentials is the raw token or "Bearer <token>".
func (h *HS256) Authenticate(_ context.Context, credentials string) (authz.Principal, error) {
	token := strings.TrimSpace(credentials)
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return authz.Principal{}, authz.ErrNoCredentials
	}
	c, err := h.verify(token)
	if err != nil {
		return authz.Principal{}, fmt.Errorf("%w: %v", authz.ErrInvalidCredentials, err)
	}
	p := authz.Principal{Name: c.Username, Kind: authz.Human, Claims: map[string]string{}}
	if strings.EqualFold(c.ActorKind, "service") {
		p.Kind = authz.Service
	}
	// Malformed ids leave zero values: the resolver denies them as an incomplete principal.
	if id, err := domain.ParseUUID(c.Subject); err == nil {
		p.Subject = id
	}
	if id, err := domain.ParseUUID(c.PartyID); err == nil {
		p.PartyID = id
	}
	if len(c.Roles) > 0 {
		p.Claims["roles"] = strings.Join(c.Roles, ",")
	}
	return p, nil
}

func (h *HS256) verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("malformed token")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := decode(parts[0], &header); err != nil {
		return Claims{}, err
	}
	if header.Alg != "HS256" { // rejects "none" and algorithm confusion
		return Claims{}, fmt.Errorf("unsupported alg %q", header.Alg)
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return Claims{}, errors.New("malformed signature")
	}
	if !hmac.Equal(sig, h.sign(parts[0]+"."+parts[1])) {
		return Claims{}, errors.New("invalid signature")
	}
	var c Claims
	if err := decode(parts[1], &c); err != nil {
		return Claims{}, err
	}
	now := domain.Now()
	switch {
	case c.Issuer != h.cfg.Issuer:
		return Claims{}, errors.New("invalid issuer")
	case !slices.Contains(c.Audience, h.cfg.Audience):
		return Claims{}, errors.New("invalid audience")
	case c.ExpiresAt == 0 || now.After(time.Unix(c.ExpiresAt, 0).Add(h.cfg.Skew)):
		return Claims{}, errors.New("token expired")
	case c.NotBefore != 0 && now.Before(time.Unix(c.NotBefore, 0).Add(-h.cfg.Skew)):
		return Claims{}, errors.New("token not yet valid")
	}
	return c, nil
}

// Issue signs claims, filling issuer, audience and iat when empty (used by the security
// service and by tests).
func (h *HS256) Issue(c Claims) (string, error) {
	if c.Issuer == "" {
		c.Issuer = h.cfg.Issuer
	}
	if len(c.Audience) == 0 {
		c.Audience = Audience{h.cfg.Audience}
	}
	if c.IssuedAt == 0 {
		c.IssuedAt = domain.Now().Unix()
	}
	head, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	unsigned := b64.EncodeToString(head) + "." + b64.EncodeToString(body)
	return unsigned + "." + b64.EncodeToString(h.sign(unsigned)), nil
}

func (h *HS256) sign(s string) []byte {
	m := hmac.New(sha256.New, h.cfg.Secret)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func decode(seg string, v any) error {
	raw, err := b64.DecodeString(seg)
	if err != nil || json.Unmarshal(raw, v) != nil {
		return errors.New("malformed segment")
	}
	return nil
}
