package parties_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// Identidad y perfil de cada empresa (D-122) por HTTP, en memoria y en SQLite.
func TestParties_IdentityAndProfile(t *testing.T) {
	t.Run("memory", func(t *testing.T) { compose(t, newMemoryDirectory).identityAndProfile() })
	t.Run("sqlite", func(t *testing.T) {
		e := compose(t, newMemoryDirectory)
		e.toSQLite()
		e.identityAndProfile()
	})
}

// toSQLite cambia el almacén del anfitrión de prueba a una base SQLite nueva y migrada.
func (e *env) toSQLite() {
	t, ctx := e.t, context.Background()
	// synchronous(OFF): sin un fsync por escritura; la base es desechable y el outbox se vacía entero.
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "parties.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(OFF)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, _ := infrastructure.Migrator(db)
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func (e *env) identityAndProfile() {
	t := e.t
	t.Helper()
	company := func(name string) papp.PartyDTO {
		var p papp.PartyDTO
		e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": name, "roles": []string{domain.RoleInternalOrganization.String()}}, &p),
			201, "register "+name)
		return p
	}
	acme, beta := company("Acme Identidad"), company("Beta Identidad")
	e.grant("clerk", authz.Full, acme.ID)  // administrador de Acme: tiene UpdateRelated
	e.grant("seller", authz.Full, acme.ID) // usuario estándar de Acme
	e.grant("reader", authz.ReadOnly, acme.ID)
	e.grant("outsider", authz.Full, beta.ID) // de Beta
	var talleres papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Talleres Comunes SL"}, &talleres), 201, "a third party")
	relate := func(typ domain.RelationshipTypeID, from, to string) papp.RelationshipDTO {
		var r papp.RelationshipDTO
		e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": typ.String(), "fromParty": from, "toParty": to}, &r), 201, "relate")
		return r
	}
	acmeSells := relate(domain.RelCustomer, talleres.ID, acme.ID)
	relate(domain.RelCustomer, talleres.ID, beta.ID)
	acmeBuys := relate(domain.RelSupplier, acme.ID, talleres.ID)
	contacts := "/api/parties/" + talleres.ID + "/contacts"

	// 1. Contactos con dueña. El vendedor no puede cambiar a Talleres (sólo se relaciona con Acme):
	// lo que añade es de Acme, sin decirlo.
	var added papp.ContactResult
	e.must(e.do("POST", contacts, e.seller, map[string]any{"kind": "email", "value": "compras@talleres.example", "purposes": []string{"default"}}, &added),
		200, "the seller adds acme's contact")
	if len(added.Contacts) != 1 || added.Contacts[0].Owner != acme.ID {
		t.Fatalf("acme owns the contact: %+v", added.Contacts)
	}
	acmeMail := added.Contacts[0].ID
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "email", "value": "info@talleres.example"}, nil), 200, "a contact of everybody")
	var bad papp.ContactResult
	e.must(e.do("POST", contacts, e.seller, map[string]any{"kind": "phone", "value": "600", "owner": acme.ID}, &bad), 200, "a bad phone of acme")
	if len(bad.Warnings) != 1 || len(bad.DataQualityIssues) != 1 {
		t.Fatalf("acme sees its issue: %+v", bad)
	}
	e.must(e.do("POST", contacts, e.seller, map[string]any{"kind": "email", "value": "x@talleres.example", "owner": beta.ID}, nil), 403,
		"nobody gives a contact to an organization they do not write in")
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "email", "value": "x@talleres.example", "owner": talleres.ID}, nil), 422,
		"the owner is an internal organization")

	var seen papp.PartyDTO
	e.must(e.do("GET", "/api/parties/"+talleres.ID, e.outsider, nil, &seen), 200, "beta sees talleres")
	if len(seen.Contacts) != 1 || seen.Contacts[0].Owner != "" || len(seen.DataQualityIssues) != 0 {
		t.Fatalf("beta sees only the contact of everybody, and no issue of acme: %+v %+v", seen.Contacts, seen.DataQualityIssues)
	}
	e.must(e.do("GET", "/api/parties/"+talleres.ID, e.reader, nil, &seen), 200, "a reader of acme")
	if len(seen.Contacts) != 3 {
		t.Fatalf("a read-only grant on acme sees acme's contacts: %+v", seen.Contacts)
	}
	e.must(e.do("POST", contacts+"/"+acmeMail+"/end", e.outsider, nil, nil), 404, "beta cannot end what it does not see")
	e.must(e.do("PUT", contacts+"/"+acmeMail+"/purposes", e.outsider, map[string]any{"purposes": []string{"billing"}}, nil), 404, "nor change it")
	var page fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?withDataQualityIssues=true&q=Talleres%20Comunes", e.outsider, nil, &page), 200, "beta searches issues")
	if page.Total != 0 {
		t.Fatalf("an issue of acme's contact does not show talleres to beta: %+v", page.Items)
	}
	e.must(e.do("GET", "/api/parties?withDataQualityIssues=true&q=Talleres%20Comunes", e.seller, nil, &page), 200, "acme searches issues")
	if page.Total != 1 {
		t.Fatalf("acme finds it: %+v", page.Items)
	}
	e.must(e.do("PUT", contacts+"/"+acmeMail+"/purposes", e.seller, map[string]any{"purposes": []string{"billing"}}, nil), 200, "acme changes its contact")
	e.must(e.do("POST", contacts+"/"+bad.Contacts[len(bad.Contacts)-1].ID+"/end", e.seller, nil, nil), 200, "and ends its bad phone")
	e.grant("seller", authz.Full, beta.ID)
	e.must(e.do("POST", contacts, e.seller, map[string]any{"kind": "email", "value": "y@talleres.example"}, nil), 400,
		"writing in two companies related to talleres, say which owns the contact")

	// 6. Quién corrige la identidad.
	var mine papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.seller, map[string]any{"legalName": "Propia SL", "tradeName": "Propia", "language": "ca",
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &mine), 201, "acme registers a customer")
	if mine.Language != "ca" {
		t.Fatalf("the language of the registration: %+v", mine)
	}
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "X", "firstSurname": "Y", "language": "xx",
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, nil), 400, "xx is no language")
	party := "/api/parties/" + mine.ID
	e.must(e.do("PUT", party+"/language", e.seller, map[string]any{"language": "EN"}, &mine), 200, "the language is changed by who changes the party")
	if mine.Language != "en" {
		t.Fatalf("language: %q", mine.Language)
	}
	cif := map[string]any{"documentType": domain.DocTaxID.String(), "country": "ES", "number": "B12345674"}
	e.must(e.do("POST", party+"/identifications", e.seller, cif, &mine), 200, "the first document completes the identity")
	other := map[string]any{"documentType": domain.DocOther.String(), "country": "ES", "number": "REG-1"}
	e.must(e.do("POST", party+"/identifications", e.seller, other, nil), 403, "a second one corrects it: only an administrator")
	e.must(e.do("POST", party+"/identifications", e.clerk, other, &mine), 200, "the administrator of acme adds it")
	e.must(e.do("PUT", party+"/identifications/"+mine.Identifications[1].ID+"/primary", e.seller, nil, nil), 403, "nor marks the primary")
	e.must(e.do("PUT", party+"/identifications/"+mine.Identifications[1].ID+"/primary", e.clerk, nil, &mine), 200, "the administrator does")
	if !mine.Identifications[1].Primary || mine.Identifications[0].Primary {
		t.Fatalf("primary: %+v", mine.Identifications)
	}
	e.must(e.do("DELETE", party+"/identifications/"+mine.Identifications[1].ID, e.seller, nil, nil), 403, "nor removes a document")
	e.must(e.do("PUT", party+"/name", e.seller, map[string]any{"legalName": "Propia SL", "tradeName": "La Propia"}, nil), 200, "the trade name is not restricted")
	e.must(e.do("PUT", party+"/name", e.seller, map[string]any{"legalName": "Propia Sociedad Limitada", "tradeName": "La Propia"}, nil), 403,
		"the legal name is")
	e.must(e.do("PUT", party+"/name", e.clerk, map[string]any{"legalName": "Propia Sociedad Limitada", "tradeName": "La Propia"}, nil), 200,
		"an administrator corrects it")
	e.must(e.do("PUT", "/api/parties/"+acme.ID+"/name", e.clerk, map[string]any{"legalName": "Acme Identidad SA"}, nil), 200,
		"the users of an internal organization correct its name")
	e.must(e.do("PUT", "/api/parties/"+beta.ID+"/name", e.clerk, map[string]any{"legalName": "Beta Nope"}, nil), 404, "not another's")

	// La marca de documento rechazado también es identidad: sólo la pone y la quita un administrador.
	e.must(e.do("PUT", party+"/document-issue", e.seller, map[string]any{"reason": "dígito de control"}, nil), 403, "the seller does not flag the document")
	e.must(e.do("PUT", party+"/document-issue", e.clerk, map[string]any{"reason": "dígito de control"}, nil), 200, "the administrator does")
	e.must(e.do("DELETE", party+"/document-issue", e.seller, nil, nil), 403, "nor clears it")
	e.must(e.do("DELETE", party+"/document-issue", e.clerk, nil, nil), 200, "the administrator clears it")

	// Un contacto de una empresa no va a una organización interna de otros ni al catálogo compartido.
	relate(domain.RelCustomer, beta.ID, acme.ID) // Beta compra a Acme: Acme la ve
	e.must(e.do("POST", "/api/parties/"+beta.ID+"/contacts", e.clerk, map[string]any{"kind": "email", "value": "a@beta.example", "owner": acme.ID}, nil), 403,
		"acme puts no contact of its own in beta")
	var catalog papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Banco Compartido"}, &catalog), 201, "a shared entry")
	e.must(e.do("PUT", "/api/parties/"+catalog.ID+"/shared", e.admin, map[string]any{"shared": true}, nil), 200, "shared")
	e.must(e.do("POST", "/api/parties/"+catalog.ID+"/contacts", e.clerk, map[string]any{"kind": "email", "value": "a@banco.example", "owner": acme.ID}, nil), 403,
		"nor in the shared catalog")
	e.must(e.do("POST", "/api/parties/"+catalog.ID+"/contacts", e.admin, map[string]any{"kind": "email", "value": "a@banco.example", "owner": acme.ID}, nil), 200,
		"the global administrator can")

	// La provincia fiscal sale de los contactos de todos, no de los de una empresa.
	postal := func(who, cp, owner string) {
		body := map[string]any{"kind": "postal", "address": map[string]any{"line1": "Mayor", "postalCode": cp, "country": "ES"}}
		if owner != "" {
			body["owner"] = owner
		}
		e.must(e.do("POST", contacts, who, body, nil), 200, "a postal address "+cp)
	}
	province := func() string {
		ti, err := e.mod.TaxIdentities.TaxIdentities(context.Background(), []string{talleres.ID})
		if err != nil {
			t.Fatal(err)
		}
		return ti[talleres.ID].Province
	}
	postal(e.clerk, "28001", acme.ID)
	if p := province(); p != "" {
		t.Fatalf("acme's address is not the fiscal address: %q", p)
	}
	postal(e.admin, "08001", "")
	if p := province(); p != "08" {
		t.Fatalf("the address of everybody is: %q", p)
	}

	// 3. Número de proveedor y códigos externos.
	var rel papp.RelationshipDTO
	e.must(e.do("PUT", "/api/party-relationships/"+acmeBuys.ID+"/supplier-number", e.clerk, map[string]any{"supplierNumber": "p-17"}, &rel), 200, "supplier number")
	if rel.Supplier == nil || rel.Supplier.Number != "P-17" {
		t.Fatalf("supplier: %+v", rel.Supplier)
	}
	ferreteria := relate(domain.RelSupplier, acme.ID, mine.ID)
	e.must(e.do("PUT", "/api/party-relationships/"+ferreteria.ID+"/supplier-number", e.clerk, map[string]any{"supplierNumber": "P-17"}, nil), 422,
		"another supplier of acme has it")
	e.must(e.do("PUT", "/api/party-relationships/"+acmeSells.ID+"/supplier-number", e.clerk, map[string]any{"supplierNumber": "P-1"}, nil), 422,
		"a customer has no supplier number")
	e.must(e.do("PUT", "/api/party-relationships/"+acmeBuys.ID+"/supplier-number", e.outsider, map[string]any{"supplierNumber": "P-2"}, nil), 404,
		"beta does not see acme's pair")
	e.must(e.do("PUT", "/api/party-relationships/"+acmeSells.ID+"/external-codes", e.seller, map[string]any{"sageCode": "430000017", "apiscoreCode": "C-77"}, &rel),
		200, "external codes")
	if rel.Customer == nil || rel.Customer.SageCode != "430000017" || rel.Customer.ApiscoreCode != "C-77" {
		t.Fatalf("customer: %+v", rel.Customer)
	}
	e.must(e.do("PUT", "/api/party-relationships/"+acmeBuys.ID+"/external-codes", e.seller, map[string]any{"sageCode": "4000000170000000"}, nil), 400,
		"a Sage code has at most 15")

	// 5. Consentimientos de protección de datos de la pareja.
	consents := "/api/party-relationships/" + acmeSells.ID + "/data-protection-consents"
	e.must(e.do("POST", consents, e.seller, map[string]any{"legalBasis": "consent", "purpose": "marketing", "retentionMonths": 24}, &rel), 200, "a consent")
	if len(rel.DataProtectionConsents) != 1 || !rel.DataProtectionConsents[0].Active || rel.DataProtectionConsents[0].RetentionMonths != 24 {
		t.Fatalf("consents: %+v", rel.DataProtectionConsents)
	}
	consent := rel.DataProtectionConsents[0].ID
	e.must(e.do("POST", consents, e.seller, map[string]any{"legalBasis": "consent", "purpose": "Marketing"}, nil), 422, "one in force per purpose")
	e.must(e.do("POST", consents, e.seller, map[string]any{"legalBasis": "whim", "purpose": "x"}, nil), 400, "a legal basis of the GDPR")
	e.must(e.do("POST", consents, e.outsider, map[string]any{"legalBasis": "contract", "purpose": "x"}, nil), 404, "beta does not see acme's pair")
	e.must(e.do("POST", consents+"/"+consent+"/withdraw", e.outsider, nil, nil), 404, "nor withdraws its consent")
	future := fw.Now().Add(time.Hour)
	e.must(e.do("POST", consents+"/"+consent+"/withdraw", e.seller, map[string]any{"at": future}, nil), 400, "not withdrawn in the future")
	e.must(e.do("POST", consents+"/"+consent+"/withdraw", e.seller, nil, &rel), 200, "acme withdraws it")
	if rel.DataProtectionConsents[0].Active || rel.DataProtectionConsents[0].WithdrawnAt == nil {
		t.Fatalf("withdrawn: %+v", rel.DataProtectionConsents)
	}
	var rels []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+talleres.ID+"/relationships", e.reader, nil, &rels), 200, "talleres' relationships")
	if i := slices.IndexFunc(rels, func(r papp.RelationshipDTO) bool { return r.ID == acmeSells.ID }); i < 0 ||
		len(rels[i].DataProtectionConsents) != 1 || rels[i].Customer.SageCode != "430000017" {
		t.Fatalf("read back: %+v", rels)
	}

	// El perfil de la pareja es de su empresa: Beta, cliente de Acme, ve la relación pero no lo que
	// Acme anota en ella.
	var rs []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+beta.ID+"/relationships", e.admin, nil, &rs), 200, "beta's relationships")
	betaBuys := rs[slices.IndexFunc(rs, func(r papp.RelationshipDTO) bool {
		return r.Type == domain.RelCustomer.String() && r.ToParty == acme.ID
	})]
	e.must(e.do("PUT", "/api/party-relationships/"+betaBuys.ID+"/external-codes", e.clerk, map[string]any{"sageCode": "430000099"}, nil), 200, "acme's code for beta")
	e.must(e.do("POST", "/api/party-relationships/"+betaBuys.ID+"/data-protection-consents", e.clerk, map[string]any{"legalBasis": "contract",
		"purpose": "billing"}, nil), 200, "acme's consent for beta")
	e.must(e.do("GET", "/api/parties/"+beta.ID+"/relationships", e.outsider, nil, &rs), 200, "beta reads its relationships")
	if i := slices.IndexFunc(rs, func(r papp.RelationshipDTO) bool { return r.ID == betaBuys.ID }); i < 0 || rs[i].Customer == nil ||
		rs[i].Customer.SageCode != "" || len(rs[i].DataProtectionConsents) != 0 {
		t.Fatalf("beta does not see acme's profile of the pair: %+v", rs)
	}
	e.must(e.do("GET", "/api/parties/"+beta.ID+"/relationships", e.clerk, nil, &rs), 200, "acme reads them")
	if i := slices.IndexFunc(rs, func(r papp.RelationshipDTO) bool { return r.ID == betaBuys.ID }); i < 0 || rs[i].Customer.SageCode != "430000099" ||
		len(rs[i].DataProtectionConsents) != 1 {
		t.Fatalf("acme sees it: %+v", rs)
	}

	// Los avisos: las correcciones de identidad y los consentimientos, publicados.
	broker := inprocess.NewBroker()
	store := memory.NewStore("crm")
	crm := messaging.NewConsumer("crm", memory.NewInbox(store), store)
	var corrected []contracts.PartyIdentityCorrectedV1
	messaging.Handle(crm, func(_ context.Context, ev contracts.PartyIdentityCorrectedV1, _ application.Envelope) error {
		if ev.PartyID == mine.ID {
			corrected = append(corrected, ev)
		}
		return nil
	})
	var recorded, withdrawn int
	messaging.Handle(crm, func(_ context.Context, ev contracts.DataProtectionConsentRecordedV1, _ application.Envelope) error {
		if ev.Organization == acme.ID && ev.PartyID == talleres.ID && ev.Purpose == "marketing" {
			recorded++
		}
		return nil
	})
	messaging.Handle(crm, func(_ context.Context, ev contracts.DataProtectionConsentWithdrawnV1, _ application.Envelope) error {
		withdrawn++
		return nil
	})
	var owned int
	messaging.Handle(crm, func(_ context.Context, ev contracts.PartyContactAddedV1, _ application.Envelope) error {
		if ev.Owner == acme.ID {
			owned++
		}
		return nil
	})
	broker.Subscribe("crm", crm)
	if _, err := e.mod.Relay(broker, outbox.WithBatchSize(10000)).RelayOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// El idioma; el segundo documento; el principal; el nombre comercial; la razón social; la marca
	// de documento rechazado, puesta y quitada. El primer documento completa, no corrige: no avisa.
	var fields []string
	for _, c := range corrected {
		fields = append(fields, strings.Join(c.Fields, "+"))
	}
	want := []string{"language", "identifications", "identifications", "tradeName", "name", "documentIssue", "documentIssue"}
	if !slices.Equal(fields, want) || corrected[4].CorrectedBy != "carl.clerk" || !slices.Contains(corrected[4].Organizations, acme.ID) {
		t.Fatalf("identity corrections: %v %+v", fields, corrected)
	}
	if recorded != 1 || withdrawn != 1 || owned != 4 { // el correo, el teléfono y la dirección de Acme en Talleres, y el del catálogo
		t.Fatalf("consents recorded %d, withdrawn %d; contacts of acme %d", recorded, withdrawn, owned)
	}
}
