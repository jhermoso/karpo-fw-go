package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// CodeIdempotencyKeyReused es el código de regla que se devuelve cuando quien llama repite una
// clave de idempotencia con una petición distinta.
const CodeIdempotencyKeyReused = "idempotency.key_reused"

// idempotentRecord es lo que guarda el almacén de una petición procesada: la huella de la
// petición que produjo el resultado, y el resultado.
type idempotentRecord struct {
	Request string          `json:"request"`
	Result  json.RawMessage `json:"result"`
}

// Idempotent devuelve el resultado guardado de una petición cuya clave de idempotencia
// (application.IdempotencyKeyed) ya se procesó con éxito, en lugar de ejecutarla otra vez
// (equivale al IdempotencyCommandHandlerDecorator de C#). Los resultados se guardan como JSON
// durante ttl. Las peticiones sin clave (o con la clave vacía) pasan sin más.
//
// Una clave pertenece a quien la usó: la misma clave enviada por otro llamante es otra petición,
// así que nadie recibe el resultado de otro. Repetir una clave con una petición distinta es una
// violación de regla (CodeIdempotencyKeyReused) y no una repetición del primer resultado.
//
// El resultado se guarda después de que next vuelva, así que no forma parte de la transacción del
// caso de uso: un proceso que se detiene entre el commit y el guardado vuelve a ejecutar la petición
// al reintentar, y dos peticiones simultáneas con la misma clave pueden ejecutarse ambas. Cerrar esa
// ventana necesita un almacén que escriba en la unidad de trabajo (BACKLOG.md, "Idempotencia persistente").
func Idempotent[In, Out any](store application.IdempotencyStore, ttl time.Duration) application.Middleware[In, Out] {
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			keyed, ok := any(in).(application.IdempotencyKeyed)
			if !ok || keyed.IdempotencyKey() == "" {
				return next.Handle(ctx, in)
			}
			key := fmt.Sprintf("%T:%q:%s", in, idempotencyCaller(ctx), keyed.IdempotencyKey())

			var out Out
			body, err := json.Marshal(in)
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding request: %w", err)
			}
			sum := sha256.Sum256(body)
			fingerprint := hex.EncodeToString(sum[:])

			if raw, found, err := store.Get(ctx, key); err != nil {
				return out, err
			} else if found {
				var rec idempotentRecord
				if err := json.Unmarshal(raw, &rec); err != nil {
					return out, fmt.Errorf("idempotency: decoding stored record: %w", err)
				}
				if rec.Request != fingerprint {
					return out, domain.Violation(CodeIdempotencyKeyReused,
						"the idempotency key was already used with a different request")
				}
				if err := json.Unmarshal(rec.Result, &out); err != nil {
					return out, fmt.Errorf("idempotency: decoding stored result: %w", err)
				}
				return out, nil
			}

			out, err = next.Handle(ctx, in)
			if err != nil {
				return out, err
			}
			result, err := json.Marshal(out)
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding result: %w", err)
			}
			raw, err := json.Marshal(idempotentRecord{Request: fingerprint, Result: result})
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding record: %w", err)
			}
			return out, store.Put(ctx, key, raw, ttl)
		})
	}
}

// idempotencyCaller identifica quién hace la petición: el sujeto autenticado cuando hay un
// contexto de autorización y, si no, el actor de la petición.
func idempotencyCaller(ctx context.Context) string {
	if c, ok := authz.FromContext(ctx); ok && !c.Subject.IsZero() {
		return c.TenantID + "/" + c.Subject.String()
	}
	a := application.ActorFrom(ctx)
	if !a.PartyID.IsZero() {
		return a.PartyID.String()
	}
	return a.Name
}
