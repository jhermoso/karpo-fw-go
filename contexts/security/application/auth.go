package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Login signs in with a user name and a password (one way of authenticating among several).
type Login struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Refresh exchanges a refresh token for a new pair of tokens.
type Refresh struct {
	RefreshToken string `json:"refreshToken"`
}

// Logout ends the session of a refresh token.
type Logout struct {
	RefreshToken string `json:"refreshToken"`
}

// ChangePassword changes the password of the caller.
type ChangePassword struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// Tokens is the result of signing in. The access token only says who the caller is: permissions
// and organization accesses are resolved on every request and read with GET /api/auth/context.
type Tokens struct {
	AccessToken        string    `json:"accessToken"`
	RefreshToken       string    `json:"refreshToken"`
	ExpiresAt          time.Time `json:"expiresAt"`
	UserID             string    `json:"userId"`
	Username           string    `json:"username"`
	MustChangePassword bool      `json:"mustChangePassword"`
}

// Done is the result of commands that return nothing.
type Done struct{}

// errSignIn is the single answer to every failed sign-in: an unknown user, a wrong password, an
// inactive or locked user and a user without password are indistinguishable from outside.
var errSignIn = authz.ErrInvalidCredentials

func actorOf(u *domain.User) vocab.Actor {
	return vocab.Actor{PartyID: u.Party().UUID, Name: u.Username()}
}

func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// endSessions ends the live sessions matching s.
func (s *service) endSessions(ctx context.Context, sp spec.Specification[*domain.Session], reason string, at time.Time) error {
	live, err := s.Sessions.Find(ctx, sp)
	if err != nil {
		return err
	}
	for _, sess := range live {
		if sess.End(reason, at) {
			if err := s.Sessions.Save(ctx, sess); err != nil {
				return err
			}
		}
	}
	return nil
}

// openSession opens a session of the family and issues the pair of tokens.
func (s *service) openSession(ctx context.Context, u *domain.User, family fw.UUID, at time.Time) (Tokens, error) {
	refresh, err := newRefreshToken()
	if err != nil {
		return Tokens{}, err
	}
	sess, err := domain.OpenSession(u.ID(), family, refresh, at, s.SessionTTL)
	if err != nil {
		return Tokens{}, err
	}
	if err := s.Sessions.Save(ctx, sess); err != nil {
		return Tokens{}, err
	}
	expires := at.Add(s.AccessTTL)
	access, err := s.Tokens.IssueAccessToken(ctx, u.ID().UUID, u.Username(), u.Party().UUID, expires)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: refresh, ExpiresAt: expires, UserID: u.ID().String(),
		Username: u.Username(), MustChangePassword: u.MustChangePassword()}, nil
}

// failedLogin counts a failed password check; a user that becomes locked loses its sessions.
func (s *service) failedLogin(ctx context.Context, id domain.UserID, at time.Time) error {
	_, err := s.users.Update(ctx, id, func(ctx context.Context, u *domain.User) error {
		if u.RecordFailedLogin(s.Lockout, at) {
			return s.endSessions(ctx, domain.LiveSessionsOf(u.ID()), domain.EndedLocked, at)
		}
		return nil
	})
	return err
}

func (s *service) login(ctx context.Context, c Login) (Tokens, error) {
	now := fw.Now()
	u, found, err := first(s.Users.Find(ctx, domain.UserNamed(c.Username)))
	if err != nil {
		return Tokens{}, err
	}
	if !found || !u.HasPassword() || !u.CanSignInAt(now) {
		s.Hasher.Verify(c.Password, s.dummyHash) // the same work whatever the reason
		return Tokens{}, errSignIn
	}
	ok, stale := s.Hasher.Verify(c.Password, u.PasswordHash())
	ctx = app.WithActor(ctx, actorOf(u))
	if !ok {
		if err := s.failedLogin(ctx, u.ID(), now); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, errSignIn
	}
	var upgraded string
	if stale {
		if upgraded, err = s.Hasher.Hash(c.Password); err != nil {
			return Tokens{}, err
		}
	}
	var out Tokens
	err = s.UoW.Do(ctx, func(ctx context.Context) error {
		u, err := s.users.Update(ctx, u.ID(), func(_ context.Context, u *domain.User) error {
			if !u.CanSignInAt(now) {
				return errSignIn
			}
			u.RecordLogin(now)
			u.UpgradePasswordHash(upgraded)
			return nil
		})
		if err != nil {
			return err
		}
		out, err = s.openSession(ctx, u, fw.NewUUID(), now)
		return err
	})
	return out, err
}

func (s *service) refresh(ctx context.Context, c Refresh) (Tokens, error) {
	now := fw.Now()
	sess, found, err := first(s.Sessions.Find(ctx, domain.SessionOfToken(c.RefreshToken)))
	if err != nil {
		return Tokens{}, err
	}
	if !found {
		return Tokens{}, errSignIn
	}
	if !sess.UsableAt(now) {
		if sess.WasRotated() { // a token that was already exchanged: the family is compromised
			err := s.UoW.Do(ctx, func(ctx context.Context) error {
				return s.endSessions(ctx, domain.LiveSessionsOfFamily(sess.Family()), domain.EndedReuse, now)
			})
			if err != nil {
				return Tokens{}, err
			}
		}
		return Tokens{}, errSignIn
	}
	u, err := s.Users.Get(ctx, sess.User())
	if err != nil {
		return Tokens{}, err
	}
	var out Tokens
	usable := u.CanSignInAt(now)
	err = s.UoW.Do(ctx, func(ctx context.Context) error {
		reason := domain.EndedRotated
		if !usable {
			reason = domain.EndedDeactivated
		}
		sess.End(reason, now)
		if err := s.Sessions.Save(ctx, sess); err != nil {
			return err
		}
		if !usable {
			return nil
		}
		out, err = s.openSession(ctx, u, sess.Family(), now)
		return err
	})
	switch {
	case errors.Is(err, fw.ErrConflict): // the same token exchanged twice at once: one wins
		return Tokens{}, errSignIn
	case err != nil:
		return Tokens{}, err
	case !usable:
		return Tokens{}, errSignIn
	}
	return out, nil
}

func (s *service) logout(ctx context.Context, c Logout) (Done, error) {
	return Done{}, s.UoW.Do(ctx, func(ctx context.Context) error {
		return s.endSessions(ctx, domain.SessionOfToken(c.RefreshToken).And(domain.SessionFieldEndedAt.IsNull()), domain.EndedLogout, fw.Now())
	})
}

func (s *service) changePassword(ctx context.Context, c ChangePassword) (Tokens, error) {
	ac, ok := authz.FromContext(ctx)
	if !ok || ac.Kind != authz.Human {
		return Tokens{}, fw.ErrUnauthorized
	}
	now := fw.Now()
	id := domain.UserID{UUID: ac.Subject}
	u, err := s.Users.Get(ctx, id)
	if err != nil {
		return Tokens{}, err
	}
	if !u.HasPassword() {
		return Tokens{}, fw.Violation("security.no_password", "this user signs in with an external identity and has no password")
	}
	if ok, _ := s.Hasher.Verify(c.CurrentPassword, u.PasswordHash()); !ok {
		if err := s.failedLogin(ctx, id, now); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, fw.Violation("security.wrong_password", "the current password is not correct")
	}
	if err := domain.CheckPassword(c.NewPassword, u.Username()); err != nil {
		return Tokens{}, err
	}
	if c.NewPassword == c.CurrentPassword {
		return Tokens{}, fw.Violation("security.same_password", "the new password must be different from the current one")
	}
	hash, err := s.Hasher.Hash(c.NewPassword)
	if err != nil {
		return Tokens{}, err
	}
	var out Tokens
	err = s.UoW.Do(ctx, func(ctx context.Context) error {
		u, err := s.users.Update(ctx, id, func(ctx context.Context, u *domain.User) error {
			if err := u.SetPassword(hash, false, false); err != nil {
				return err
			}
			return s.endSessions(ctx, domain.LiveSessionsOf(id), domain.EndedPassword, now)
		})
		if err != nil {
			return err
		}
		out, err = s.openSession(ctx, u, fw.NewUUID(), now)
		return err
	})
	return out, err
}
