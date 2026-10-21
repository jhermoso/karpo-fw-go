package receivables_test

import (
	"context"
	"encoding/json"
	"testing"

	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
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

// Al fusionar fichas, el perfil de crédito de la pareja pasa a la superviviente si no tiene uno
// con ese vendedor (D-93); el de la ficha fusionada se queda, y una reentrega no hace nada.
func TestReceivables_CreditFollowsAMergedCustomer(t *testing.T) {
	ctx := adminContext(t)
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := receivables.Compose(sw, nil)
	seller, ceding, receiving, other := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	limit := "5000"
	if _, err := m.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: seller, Customer: ceding, Limit: &limit, Blocked: true}); err != nil {
		t.Fatal(err)
	}
	small := "10"
	if _, err := m.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: seller, Customer: other, Limit: &small}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []application.Envelope{transferred(t, "customer", seller, ceding, receiving), transferred(t, "customer", seller, ceding, receiving),
		transferred(t, "customer", seller, ceding, other), transferred(t, "supplier", seller, other, fw.NewUUID().String())} {
		if err := m.Consumer.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := m.Service.SearchCredit.Handle(ctx, rapp.SearchCredit{Seller: seller})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]rapp.CreditDTO{}
	for _, p := range ps {
		got[p.Customer] = p
	}
	if len(ps) != 3 || got[receiving].Limit != "5000.00" || !got[receiving].Blocked || got[ceding].Limit != "5000.00" || got[other].Limit != "10.00" {
		t.Fatalf("credit profiles after the merge: %+v", ps)
	}
	// Al deshacer la fusión la relación vuelve (Ceding la superviviente, Receiving la fusionada): no
	// se copia nada, ni a la que tenía perfil ni a la que no.
	more := "7000"
	if _, err := m.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: seller, Customer: receiving, Limit: &more}); err != nil {
		t.Fatal(err)
	}
	fresh := fw.NewUUID().String()
	for _, env := range []application.Envelope{returned(t, "customer", seller, receiving, ceding), returned(t, "customer", seller, receiving, fresh)} {
		if err := m.Consumer.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	if ps, err = m.Service.SearchCredit.Handle(ctx, rapp.SearchCredit{Seller: seller}); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		got[p.Customer] = p
	}
	if _, copied := got[fresh]; len(ps) != 3 || got[ceding].Limit != "5000.00" || copied {
		t.Fatalf("credit profiles after undoing the merge: %+v", ps)
	}
}
