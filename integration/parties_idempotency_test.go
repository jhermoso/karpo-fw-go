package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// partiesIdempotency comprueba la migración 31 de Parties en cada motor (docs/DECISIONES-COMUNES.md,
// D-125): la misma alta repetida devuelve la misma party, también con las dos peticiones a la vez;
// otra petición con la misma clave se rechaza; un alta que falla no deja la clave gastada, y la
// purga borra lo caducado. ctx es el de un administrador global.
func partiesIdempotency(ctx context.Context, t *testing.T, svc *papp.Service, ledger application.IdempotencyLedger) {
	t.Helper()
	c := papp.RegisterOrganization{LegalName: "Alta Repetida SL", RequestID: "alta-" + fw.NewUUID().String()}
	first, err := svc.RegisterOrganization.Handle(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.RegisterOrganization.Handle(ctx, c)
	if err != nil || again.ID != first.ID {
		t.Fatalf("the repeated registration: %+v %v", again, err)
	}
	other := c
	other.LegalName = "Otra Sociedad SL"
	if _, err := svc.RegisterOrganization.Handle(ctx, other); !errors.Is(err, fw.ErrRuleViolation) {
		t.Fatalf("the same key with another request: %v", err)
	}

	// Dos peticiones simultáneas con la misma clave: una espera a la otra y devuelve su party.
	both := papp.RegisterOrganization{LegalName: "Alta Simultanea SL", RequestID: "alta-" + fw.NewUUID().String()}
	ids := make([]string, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			got, err := svc.RegisterOrganization.Handle(ctx, both)
			ids[i], errs[i] = got.ID, err
		})
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("simultaneous registrations: %v %v", ids, errs)
		}
	}

	// Un alta que falla deshace su reserva: la clave sigue libre.
	bad := papp.RegisterOrganization{LegalName: "Con Rol Falso SL", Roles: []string{"not-a-role"}, RequestID: "alta-" + fw.NewUUID().String()}
	if _, err := svc.RegisterOrganization.Handle(ctx, bad); err == nil {
		t.Fatal("a registration with an unknown role must fail")
	}
	bad.Roles = nil
	if _, err := svc.RegisterOrganization.Handle(ctx, bad); err != nil {
		t.Fatalf("the key of a failed registration is free: %v", err)
	}

	// El registro, sin pasar por el servicio: lee lo que escribe y purga lo caducado.
	subject, past := fw.NewUUID(), fw.Now().Add(-time.Hour).Truncate(time.Millisecond)
	e := application.IdempotencyEntry{Tenant: "t", Subject: subject, Key: "k", RequestHash: "h", ResourceID: fw.NewUUID(), Status: 201,
		CreatedAt: past, ExpiresAt: past}
	if err := ledger.Reserve(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Reserve(ctx, e); !errors.Is(err, fw.ErrConflict) {
		t.Fatalf("the same key twice: %v", err)
	}
	got, found, err := ledger.Find(ctx, "t", subject, "k")
	if err != nil || !found || got.ResourceID != e.ResourceID || got.Status != 201 || got.RequestHash != "h" || !got.ExpiresAt.Equal(past) {
		t.Fatalf("read back: %+v %v %v", got, found, err)
	}
	if n, err := ledger.Purge(ctx, fw.Now()); err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	if _, found, err := ledger.Find(ctx, "t", subject, "k"); err != nil || found {
		t.Fatalf("purged: %v %v", found, err)
	}
}
