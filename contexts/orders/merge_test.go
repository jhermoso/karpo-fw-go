package orders_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
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

// Al fusionar fichas, las condiciones de venta de la pareja pasan a la superviviente si no tiene
// unas con esa empresa (D-93); las de la ficha fusionada se quedan, y una reentrega no hace nada.
func TestOrders_TermsFollowAMergedCustomer(t *testing.T) {
	ctx := adminContext(t)
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := orders.Compose(sw, nil, nil)
	company, ceding, receiving, other := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	if _, err := m.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: company, Customer: ceding, Discount: "5", BlockDelivery: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: company, Customer: other, Discount: "1"}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []application.Envelope{transferred(t, "customer", company, ceding, receiving), transferred(t, "customer", company, ceding, receiving),
		transferred(t, "customer", company, ceding, other), transferred(t, "supplier", company, other, fw.NewUUID().String())} {
		if err := m.Consumer.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := m.Service.SearchTerms.Handle(ctx, oapp.SearchTerms{Company: company})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]oapp.TermsDTO{}
	for _, x := range ts {
		got[x.Customer] = x
	}
	if len(ts) != 3 || !got[receiving].BlockDelivery || got[receiving].Discount != got[ceding].Discount || got[other].Discount == got[ceding].Discount {
		t.Fatalf("terms after the merge: %+v", ts)
	}
	// Al deshacer la fusión la relación vuelve (Ceding la superviviente, Receiving la fusionada): la
	// fusionada conserva sus condiciones aunque la superviviente cambiara las suyas.
	if _, err := m.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: company, Customer: receiving, Discount: "9"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Consumer.HandleMessage(context.Background(), returned(t, "customer", company, receiving, ceding)); err != nil {
		t.Fatal(err)
	}
	if ts, err = m.Service.SearchTerms.Handle(ctx, oapp.SearchTerms{Company: company}); err != nil {
		t.Fatal(err)
	}
	for _, x := range ts {
		got[x.Customer] = x
	}
	if len(ts) != 3 || !got[ceding].BlockDelivery || got[ceding].Discount == got[receiving].Discount {
		t.Fatalf("terms after undoing the merge: %+v", ts)
	}
}
