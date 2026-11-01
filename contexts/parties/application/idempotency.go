package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

const (
	// maxIdempotencyKey es lo que cabe en la columna idempotency_key.
	maxIdempotencyKey = 128
	// statusCreated es la respuesta HTTP de un alta.
	statusCreated = 201
	// idempotencyTTL es lo que se recuerda una petición (docs/DECISIONES-COMUNES.md, D-125).
	idempotencyTTL = 24 * time.Hour
)

// once da de alta una party una sola vez por clave de idempotencia (D-125), dentro de la
// transacción del alta: la fila se reserva antes de crear nada, así que dos peticiones simultáneas
// con la misma clave no se ejecutan las dos. La segunda espera a la primera en el índice único,
// recibe fw.ErrConflict, y al reintentar encuentra lo que la primera dejó. La repetición devuelve
// la party como está ahora: la tabla no guarda copia de datos personales.
func (s service) once(ctx context.Context, c app.IdempotencyKeyed, create func(domain.PartyID) (PartyDTO, error)) (PartyDTO, error) {
	key := c.IdempotencyKey()
	ac, known := authz.FromContext(ctx)
	if key == "" || !known || s.Ledger == nil {
		return create(domain.NewPartyID())
	}
	if utf8.RuneCountInString(key) > maxIdempotencyKey {
		var v fw.Validation
		v.Add("idempotencyKey", "too_long", fmt.Sprintf("the idempotency key admits at most %d characters", maxIdempotencyKey))
		return PartyDTO{}, v.Err()
	}
	body, err := json.Marshal(c)
	if err != nil {
		return PartyDTO{}, err
	}
	sum := sha256.Sum256(append(fmt.Appendf(nil, "%T:", c), body...))
	hash := hex.EncodeToString(sum[:])

	now := fw.Now()
	prior, found, err := s.Ledger.Find(ctx, ac.TenantID, ac.Subject, key)
	if err != nil {
		return PartyDTO{}, err
	}
	if found && prior.ExpiresAt.After(now) {
		if prior.RequestHash != hash {
			return PartyDTO{}, fw.Violation(pipeline.CodeIdempotencyKeyReused, "the idempotency key was already used with a different request")
		}
		n, err := s.names(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		p, err := s.Parties.Get(ctx, domain.PartyID{UUID: prior.ResourceID})
		if err != nil {
			return PartyDTO{}, err
		}
		return n.dto(p), nil
	}
	if found {
		if err := s.Ledger.Remove(ctx, ac.TenantID, ac.Subject, key); err != nil {
			return PartyDTO{}, err
		}
	}
	id := domain.NewPartyID()
	if err := s.Ledger.Reserve(ctx, app.IdempotencyEntry{Tenant: ac.TenantID, Subject: ac.Subject, Key: key, RequestHash: hash,
		ResourceID: id.UUID, Status: statusCreated, CreatedAt: now, ExpiresAt: now.Add(idempotencyTTL)}); err != nil {
		return PartyDTO{}, err
	}
	return create(id)
}
