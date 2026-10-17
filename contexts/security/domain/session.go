package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// SessionKind es el nombre estable del tipo de agregado.
const SessionKind = "security.session"

// Motivos por los que termina una sesión.
const (
	EndedRotated     = "rotated"          // su token de refresco se cambió por uno nuevo
	EndedLogout      = "logout"           // el usuario cerró la sesión
	EndedReuse       = "reuse"            // se volvió a presentar un token ya rotado de su familia
	EndedPassword    = "password-changed" // la contraseña cambió o se restableció
	EndedDeactivated = "deactivated"      // el usuario se desactivó
	EndedLocked      = "locked"           // el usuario se bloqueó
)

// Session es un token de refresco de un usuario (el SecurityRefreshToken de C#). Sólo se guarda el
// hash del token. Cada renovación termina la sesión y abre otra en la misma familia; si vuelve a
// aparecer un token que ya se cambió, alguien lo robó y termina toda la familia.
type Session struct {
	fw.BaseAggregateRoot[SessionID]
	user      UserID
	family    fw.UUID
	tokenHash string
	issuedAt  time.Time
	expiresAt time.Time
	endedAt   *time.Time
	reason    string
}

// SessionState es el estado persistido de una sesión.
type SessionState struct {
	User      UserID
	Family    fw.UUID
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
	EndedAt   *time.Time
	Reason    string
}

// ReconstituteSession reconstruye una sesión a partir del estado persistido.
func ReconstituteSession(id SessionID, s SessionState) (*Session, error) {
	base, err := fw.NewBaseAggregateRoot(SessionKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.User.IsZero(), "user", "required", "a session belongs to a user")
	v.Require(!s.Family.IsZero(), "family", "required", "a session belongs to a family")
	v.Require(len(s.TokenHash) == sha256.Size*2, "tokenHash", "format", "the token hash is a SHA-256 in hexadecimal")
	v.Require(s.ExpiresAt.After(s.IssuedAt), "expiresAt", "range", "a session expires after it is issued")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Session{BaseAggregateRoot: base, user: s.User, family: s.Family, tokenHash: s.TokenHash,
		issuedAt: s.IssuedAt.UTC(), expiresAt: s.ExpiresAt.UTC(), endedAt: cloneTime(s.EndedAt), reason: s.Reason}, nil
}

// OpenSession abre una sesión para el token de refresco (su hash) en una familia: una familia nueva
// para un login, la familia de la sesión cambiada para una renovación.
func OpenSession(user UserID, family fw.UUID, token string, at time.Time, ttl time.Duration) (*Session, error) {
	return ReconstituteSession(NewSessionID(), SessionState{User: user, Family: family, TokenHash: HashToken(token),
		IssuedAt: at, ExpiresAt: at.Add(ttl)})
}

// HashToken devuelve la forma guardada de un token de refresco.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// User devuelve el dueño.
func (s *Session) User() UserID { return s.user }

// Family devuelve la familia.
func (s *Session) Family() fw.UUID { return s.family }

// TokenHash devuelve el hash del token de refresco.
func (s *Session) TokenHash() string { return s.tokenHash }

// IssuedAt devuelve cuándo se abrió la sesión.
func (s *Session) IssuedAt() time.Time { return s.issuedAt }

// ExpiresAt devuelve cuándo caduca la sesión.
func (s *Session) ExpiresAt() time.Time { return s.expiresAt }

// EndedAt devuelve cuándo terminó la sesión, si terminó.
func (s *Session) EndedAt() *time.Time { return cloneTime(s.endedAt) }

// Reason devuelve por qué terminó la sesión ("" mientras sigue viva).
func (s *Session) Reason() string { return s.reason }

// UsableAt dice si el token de refresco se puede cambiar en t.
func (s *Session) UsableAt(t time.Time) bool { return s.endedAt == nil && t.Before(s.expiresAt) }

// WasRotated dice si el token ya se cambió: volver a presentarlo es una reutilización.
func (s *Session) WasRotated() bool { return s.reason == EndedRotated }

// End termina la sesión. Dice si estaba viva.
func (s *Session) End(reason string, at time.Time) bool {
	if s.endedAt != nil {
		return false
	}
	s.endedAt, s.reason = cloneTime(&at), reason
	return true
}

// Campos y especificaciones de Session.
var (
	SessionFieldUser      = spec.Comparable("user_id", (*Session).User)
	SessionFieldFamily    = spec.Comparable("family", (*Session).Family)
	SessionFieldTokenHash = spec.Comparable("token_hash", (*Session).TokenHash)
	SessionFieldEndedAt   = spec.OptionalTime("ended_at", (*Session).EndedAt)
)

// SessionOfToken selecciona la sesión de un token de refresco.
func SessionOfToken(token string) spec.Spec[*Session] {
	return SessionFieldTokenHash.Eq(HashToken(token))
}

// LiveSessionsOf selecciona las sesiones de un usuario que no han terminado.
func LiveSessionsOf(user UserID) spec.Spec[*Session] {
	return SessionFieldUser.Eq(user).And(SessionFieldEndedAt.IsNull())
}

// LiveSessionsOfFamily selecciona las sesiones de una familia que no han terminado.
func LiveSessionsOfFamily(family fw.UUID) spec.Spec[*Session] {
	return SessionFieldFamily.Eq(family).And(SessionFieldEndedAt.IsNull())
}
