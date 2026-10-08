package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// SessionKind is the stable aggregate type name.
const SessionKind = "security.session"

// Reasons a session ends.
const (
	EndedRotated     = "rotated"          // its refresh token was exchanged for a new one
	EndedLogout      = "logout"           // the user signed out
	EndedReuse       = "reuse"            // a rotated token of its family was presented again
	EndedPassword    = "password-changed" // the password changed or was reset
	EndedDeactivated = "deactivated"      // the user was deactivated
	EndedLocked      = "locked"           // the user was locked
)

// Session is one refresh token of a user (the C# SecurityRefreshToken). Only the hash of the
// token is stored. Each renewal ends the session and opens another in the same family; if a
// token that was already exchanged shows up again, somebody stole it and the whole family ends.
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

// SessionState is the persisted state of a session.
type SessionState struct {
	User      UserID
	Family    fw.UUID
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
	EndedAt   *time.Time
	Reason    string
}

// ReconstituteSession rebuilds a session from persisted state.
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

// OpenSession opens a session for the refresh token (its hash) in a family: a new family for a
// login, the family of the exchanged session for a renewal.
func OpenSession(user UserID, family fw.UUID, token string, at time.Time, ttl time.Duration) (*Session, error) {
	return ReconstituteSession(NewSessionID(), SessionState{User: user, Family: family, TokenHash: HashToken(token),
		IssuedAt: at, ExpiresAt: at.Add(ttl)})
}

// HashToken returns the stored form of a refresh token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// User returns the owner.
func (s *Session) User() UserID { return s.user }

// Family returns the family.
func (s *Session) Family() fw.UUID { return s.family }

// TokenHash returns the hash of the refresh token.
func (s *Session) TokenHash() string { return s.tokenHash }

// IssuedAt returns when the session was opened.
func (s *Session) IssuedAt() time.Time { return s.issuedAt }

// ExpiresAt returns when the session expires.
func (s *Session) ExpiresAt() time.Time { return s.expiresAt }

// EndedAt returns when the session ended, if it did.
func (s *Session) EndedAt() *time.Time { return cloneTime(s.endedAt) }

// Reason returns why the session ended ("" while it is live).
func (s *Session) Reason() string { return s.reason }

// UsableAt reports whether the refresh token can be exchanged at t.
func (s *Session) UsableAt(t time.Time) bool { return s.endedAt == nil && t.Before(s.expiresAt) }

// WasRotated reports whether the token was already exchanged: presenting it again is a reuse.
func (s *Session) WasRotated() bool { return s.reason == EndedRotated }

// End ends the session. It reports whether it was live.
func (s *Session) End(reason string, at time.Time) bool {
	if s.endedAt != nil {
		return false
	}
	s.endedAt, s.reason = cloneTime(&at), reason
	return true
}

// Session fields and specifications.
var (
	SessionFieldUser      = spec.Comparable("user_id", (*Session).User)
	SessionFieldFamily    = spec.Comparable("family", (*Session).Family)
	SessionFieldTokenHash = spec.Comparable("token_hash", (*Session).TokenHash)
	SessionFieldEndedAt   = spec.OptionalTime("ended_at", (*Session).EndedAt)
)

// SessionOfToken matches the session of a refresh token.
func SessionOfToken(token string) spec.Spec[*Session] {
	return SessionFieldTokenHash.Eq(HashToken(token))
}

// LiveSessionsOf matches the sessions of a user that have not ended.
func LiveSessionsOf(user UserID) spec.Spec[*Session] {
	return SessionFieldUser.Eq(user).And(SessionFieldEndedAt.IsNull())
}

// LiveSessionsOfFamily matches the sessions of a family that have not ended.
func LiveSessionsOfFamily(family fw.UUID) spec.Spec[*Session] {
	return SessionFieldFamily.Eq(family).And(SessionFieldEndedAt.IsNull())
}
