package application_test

import (
	"context"
	"testing"

	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Sin actor de petición, ActorFrom toma el del contexto de autorización (una llamada directa al
// caso de uso); sin ninguno, el sistema; y el actor de petición manda sobre el de autorización.
func TestActorFrom_FallsBackToAuthorization(t *testing.T) {
	ctx := context.Background()
	if got := app.ActorFrom(ctx); got != vocab.SystemActor {
		t.Fatalf("sin actor: %+v", got)
	}
	who := domain.NewUUID()
	ctx = authz.WithContext(ctx, &authz.Context{Subject: who, SubjectName: "boss"})
	if got := app.ActorFrom(ctx); got.PartyID != who || got.Name != "boss" {
		t.Fatalf("desde authz: %+v", got)
	}
	svc := authz.WithContext(context.Background(), &authz.Context{Subject: who, SubjectName: "karpo-host", Kind: authz.Service, ActorPartyID: who})
	if got := app.ActorFrom(svc); got != vocab.SystemActor {
		t.Fatalf("un servicio sigue siendo System: %+v", got)
	}
	req := vocab.Actor{PartyID: domain.NewUUID(), Name: "web"}
	if got := app.ActorFrom(app.WithActor(ctx, req)); got != req {
		t.Fatalf("el de la petición manda: %+v", got)
	}
}
