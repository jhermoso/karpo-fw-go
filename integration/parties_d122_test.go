package integration

import (
	"context"
	"slices"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// partiesD122 comprueba la migración 30 de Parties en cada motor (docs/DECISIONES-COMUNES.md,
// D-122): un contacto con dueña y su incidencia, que la búsqueda filtra por la dueña; el idioma
// preferido; el número de proveedor y los códigos externos de la pareja; y un consentimiento de
// protección de datos, hijo de la relación. ctx es el de un administrador global.
func partiesD122(ctx context.Context, t *testing.T, svc *papp.Service, acme string) {
	t.Helper()
	acmeID, _ := domain.ParsePartyID(acme)
	shop, err := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Tienda Compartida SL", Language: "ca"})
	if err != nil || shop.Language != "ca" {
		t.Fatalf("a party with its language: %+v %v", shop, err)
	}
	shopID, _ := domain.ParsePartyID(shop.ID)
	if _, err := svc.SetShared.Handle(ctx, papp.SetShared{ID: shopID, Shared: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: shopID, Kind: "phone", Value: "600", Owner: acme}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get.Handle(ctx, papp.GetParty{ID: shopID})
	if err != nil || len(got.Contacts) != 1 || got.Contacts[0].Owner != acme || len(got.DataQualityIssues) != 1 || got.Language != "ca" {
		t.Fatalf("read back: %+v %v", got, err)
	}
	scoped := func(org fw.UUID) context.Context {
		ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "scoped", Kind: authz.Service,
			Permissions: []authz.Permission{authz.Wildcard}, Grants: []authz.Grant{{OrganizationID: org, Level: authz.Full}}})
		return authz.WithContext(context.Background(), ac)
	}
	for org, want := range map[fw.UUID]int64{acmeID.UUID: 1, fw.NewUUID(): 0} {
		page, err := svc.Search.Handle(scoped(org), papp.SearchParties{WithDataQualityIssues: true, Text: "Tienda Compartida"})
		if err != nil || page.Total != want {
			t.Fatalf("issues of acme's contact seen from %s: %+v %v", org, page, err)
		}
	}

	// La pareja: Acme compra a la tienda y le vende.
	buys, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelSupplier.String(), From: acme, To: shop.ID})
	if err != nil {
		t.Fatal(err)
	}
	sells, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelCustomer.String(), From: shop.ID, To: acme})
	if err != nil {
		t.Fatal(err)
	}
	buysID, _ := domain.ParseRelationshipID(buys.ID)
	sellsID, _ := domain.ParseRelationshipID(sells.ID)
	if _, err := svc.SetSupplierNumber.Handle(ctx, papp.SetSupplierNumber{ID: buysID, SupplierNumber: "P-30"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetExternalCodes.Handle(ctx, papp.SetExternalCodes{ID: buysID, SageCode: "400000030", ApiscoreCode: "AP-30"}); err != nil {
		t.Fatal(err)
	}
	rec, err := svc.RecordConsent.Handle(ctx, papp.RecordConsent{ID: sellsID, LegalBasis: "consent", Purpose: "marketing", RetentionMonths: 24,
		Notes: "firmado en tienda"})
	if err != nil || len(rec.DataProtectionConsents) != 1 {
		t.Fatalf("a consent: %+v %v", rec, err)
	}
	consentID, _ := domain.ParseConsentID(rec.DataProtectionConsents[0].ID)
	if _, err := svc.WithdrawConsent.Handle(ctx, papp.WithdrawConsent{ID: sellsID, ConsentID: consentID}); err != nil {
		t.Fatal(err)
	}
	rels, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: shopID})
	if err != nil {
		t.Fatal(err)
	}
	b := rels[slices.IndexFunc(rels, func(r papp.RelationshipDTO) bool { return r.ID == buys.ID })]
	s := rels[slices.IndexFunc(rels, func(r papp.RelationshipDTO) bool { return r.ID == sells.ID })]
	if b.Supplier == nil || b.Supplier.Number != "P-30" || b.Supplier.SageCode != "400000030" || b.Supplier.ApiscoreCode != "AP-30" {
		t.Fatalf("the supplier pair read back: %+v", b.Supplier)
	}
	if len(s.DataProtectionConsents) != 1 || s.DataProtectionConsents[0].Active || s.DataProtectionConsents[0].WithdrawnAt == nil ||
		s.DataProtectionConsents[0].RetentionMonths != 24 || s.DataProtectionConsents[0].Notes != "firmado en tienda" {
		t.Fatalf("the consent read back: %+v", s.DataProtectionConsents)
	}
	// Lo deja como estaba: la tienda deja de ser compartida y de relacionarse con Acme, para que no
	// cuente en lo que el escenario comprueba después.
	if _, err := svc.SetShared.Handle(ctx, papp.SetShared{ID: shopID, Shared: false}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.RelationshipID{buysID, sellsID} {
		if _, err := svc.TerminateRelationship.Handle(ctx, papp.TerminateRelationship{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
}
