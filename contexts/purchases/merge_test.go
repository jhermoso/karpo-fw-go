package purchases_test

import (
	"context"
	"encoding/json"
	"testing"

	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// transferred es el sobre de parties.relationship-transferred.v1 para una pareja que pasa de ceding
// a receiving con la empresa org al fusionar.
func transferred(t *testing.T, code, org, ceding, receiving string) application.Envelope {
	t.Helper()
	return envelope(t, code, org, ceding, receiving, false)
}

// returned es el mismo sobre cuando la relación vuelve al deshacer la fusión.
func returned(t *testing.T, code, org, ceding, receiving string) application.Envelope {
	t.Helper()
	return envelope(t, code, org, ceding, receiving, true)
}

func envelope(t *testing.T, code, org, ceding, receiving string, back bool) application.Envelope {
	t.Helper()
	e := parties.RelationshipTransferredV1{RelationshipID: fw.NewUUID().String(), TransferredFrom: fw.NewUUID().String(),
		Type: pdomain.RelCustomer.String(), TypeCode: code, Organization: org, CedingParty: ceding, ReceivingParty: receiving, Returning: back, At: fw.Now()}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return application.Envelope{ID: fw.NewUUID().String(), Type: e.IntegrationEventType(), Source: parties.Source, Subject: e.RelationshipID,
		OccurredAt: e.At, Data: data}
}

func adminContext(t *testing.T) context.Context {
	t.Helper()
	ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	if err != nil {
		t.Fatal(err)
	}
	ac.GlobalAdmin = true
	return authz.WithContext(context.Background(), ac)
}

// Al fusionar fichas, el perfil de proveedor de la pareja pasa a la superviviente si no tiene uno
// con esa empresa (D-93); el de la ficha fusionada se queda, y una reentrega no hace nada.
func TestPurchases_SupplierProfileFollowsAMergedSupplier(t *testing.T) {
	ctx := adminContext(t)
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := purchases.Compose(sw, nil)
	company, ceding, receiving, other := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	if _, err := m.Service.SetSupplier.Handle(ctx, uapp.SetSupplier{Company: company, Supplier: ceding, PaymentDays: 60, Blocked: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Service.SetSupplier.Handle(ctx, uapp.SetSupplier{Company: company, Supplier: other, PaymentDays: 15}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []application.Envelope{transferred(t, "supplier", company, ceding, receiving), transferred(t, "supplier", company, ceding, receiving),
		transferred(t, "supplier", company, ceding, other), transferred(t, "customer", company, other, fw.NewUUID().String())} {
		if err := m.Consumer.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := m.Service.SearchSuppliers.Handle(ctx, uapp.SearchSuppliers{Company: company})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uapp.SupplierDTO{}
	for _, p := range ps {
		got[p.Supplier] = p
	}
	if len(ps) != 3 || got[receiving].PaymentDays != 60 || !got[receiving].Blocked || got[other].PaymentDays != 15 {
		t.Fatalf("supplier profiles after the merge: %+v", ps)
	}
	// Al deshacer la fusión la relación vuelve (Ceding la superviviente, Receiving la fusionada): la
	// fusionada conserva su perfil aunque la superviviente cambiara el suyo.
	if _, err := m.Service.SetSupplier.Handle(ctx, uapp.SetSupplier{Company: company, Supplier: receiving, PaymentDays: 90}); err != nil {
		t.Fatal(err)
	}
	if err := m.Consumer.HandleMessage(context.Background(), returned(t, "supplier", company, receiving, ceding)); err != nil {
		t.Fatal(err)
	}
	if ps, err = m.Service.SearchSuppliers.Handle(ctx, uapp.SearchSuppliers{Company: company}); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		got[p.Supplier] = p
	}
	if len(ps) != 3 || got[ceding].PaymentDays != 60 || !got[ceding].Blocked {
		t.Fatalf("supplier profiles after undoing the merge: %+v", ps)
	}
}
