package authorization

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// Authenticators combina varias formas de autenticar en un solo authz.Authenticator: el token
// propio de secreto compartido, los tokens de un proveedor de identidad externo... Se prueban en
// orden y gana el primero que reconoce las credenciales.
//
// La falta de credenciales se informa en el acto. Cuando nadie las reconoce, quien llama recibe
// ErrInvalidCredentials, salvo que un autenticador haya fallado por otro motivo (su fuente no
// estaba disponible): entonces se devuelve ese error, para que una caída nunca se disfrace de token erróneo.
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
