package authorization

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// Authenticators combines several ways of authenticating into one authz.Authenticator: the own
// shared-secret token, the tokens of an external identity provider... Each one is tried in order
// and the first that recognizes the credentials wins.
//
// Missing credentials are reported at once. When nobody recognizes them the caller gets
// ErrInvalidCredentials, unless an authenticator failed for another reason (its source was
// unavailable): that error is returned, so an outage is never disguised as a wrong token.
func Authenticators(all ...authz.Authenticator) authz.Authenticator {
	return authenticators(all)
}

type authenticators []authz.Authenticator

func (as authenticators) Authenticate(ctx context.Context, credentials string) (authz.Principal, error) {
	var failure error
	for _, a := range as {
		p, err := a.Authenticate(ctx, credentials)
		switch {
		case err == nil:
			return p, nil
		case errors.Is(err, authz.ErrNoCredentials):
			return authz.Principal{}, err
		case !errors.Is(err, authz.ErrInvalidCredentials) && failure == nil:
			failure = err
		}
	}
	if failure != nil {
		return authz.Principal{}, failure
	}
	if len(as) == 0 {
		return authz.Principal{}, authz.ErrNoCredentials
	}
	return authz.Principal{}, authz.ErrInvalidCredentials
}
