package parties_test

import (
	"encoding/json"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// dataQuality cubre D-92 por HTTP: un e-mail o un teléfono con formato erróneo se guarda, la
// respuesta lo avisa en warnings, la ficha enseña la incidencia y la búsqueda la encuentra con
// withDataQualityIssues=true; terminar el contacto se la lleva. Vacío sigue siendo 400.
func (e *env) dataQuality(prefix string) {
	t := e.t
	t.Helper()
	var rita papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Rita", "firstSurname": "Recados " + prefix}, &rita), 201, "register rita")
	if rita.DataQualityIssues == nil || len(rita.DataQualityIssues) != 0 {
		t.Fatalf("a new party has no issues, and says so: %+v", rita.DataQualityIssues)
	}
	contacts := "/api/parties/" + rita.ID + "/contacts"

	var mail papp.ContactResult
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "email", "value": " rita@ ", "purposes": []string{"default"}}, &mail), 200,
		"a badly formed e-mail is kept")
	if len(mail.Warnings) != 1 || mail.Warnings[0].Field != "value" || mail.Warnings[0].Code != "INVALID_EMAIL" || mail.Warnings[0].Message == "" {
		t.Fatalf("the e-mail warning: %+v", mail.Warnings)
	}
	if len(mail.Contacts) != 1 || mail.Contacts[0].Value != "rita@" || len(mail.DataQualityIssues) != 1 {
		t.Fatalf("kept as written: %+v %+v", mail.Contacts, mail.DataQualityIssues)
	}
	if i := mail.DataQualityIssues[0]; i.Subject != mail.Contacts[0].ID || i.Code != "INVALID_EMAIL" || i.Severity != "warning" ||
		i.Field != "contact.value" || i.Value != "rita@" || i.Since.IsZero() {
		t.Fatalf("the e-mail issue: %+v", i)
	}
	var phone papp.ContactResult
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "phone", "value": "600 11 22 33"}, &phone), 200, "a phone without its prefix is kept")
	if len(phone.Warnings) != 1 || phone.Warnings[0].Code != "INVALID_PHONE" || len(phone.DataQualityIssues) != 2 {
		t.Fatalf("the phone warning: %+v %+v", phone.Warnings, phone.DataQualityIssues)
	}
	// Bien formado: warnings está, vacía.
	var raw map[string]json.RawMessage
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "email", "value": "rita@example.com"}, &raw), 200, "a good e-mail")
	if string(raw["warnings"]) != "[]" || raw["id"] == nil {
		t.Fatalf("a good e-mail answers the party and no warnings: %s", raw["warnings"])
	}
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "email", "value": "  "}, nil), 400, "an empty e-mail")
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "web", "value": "not a url"}, nil), 400, "a web address is still checked")
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "email", "value": "rita\x00@x"}, nil), 400, "a NUL is not kept")
	e.must(e.do("POST", contacts, e.admin, map[string]any{"kind": "phone", "value": "600\n112233"}, nil), 400, "nor a line break")

	var got papp.PartyDTO
	e.must(e.do("GET", "/api/parties/"+rita.ID, e.admin, nil, &got), 200, "the party shows its issues")
	if len(got.DataQualityIssues) != 2 {
		t.Fatalf("issues read back: %+v", got.DataQualityIssues)
	}
	var page fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?withDataQualityIssues=true", e.admin, nil, &page), 200, "the parties with issues")
	if page.Total != 1 || page.Items[0].ID != rita.ID {
		t.Fatalf("parties with issues: %+v", page.Items)
	}
	// El filtro se suma a los demás: una ficha fusionada con incidencias sólo sale si se pide.
	var twin papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Rita", "firstSurname": "Recados " + prefix + " bis"}, &twin), 201, "register a twin")
	e.must(e.do("POST", "/api/parties/"+twin.ID+"/contacts", e.admin, map[string]any{"kind": "email", "value": "rita@@"}, nil), 200, "the twin's bad e-mail")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(), "fromParty": twin.ID,
		"toParty": rita.ID}, nil), 201, "the twin is a duplicate of rita")
	for query, want := range map[string]int64{"withDataQualityIssues=true&active=true": 1, "withDataQualityIssues=true&includeMergedParties=true": 2,
		"withDataQualityIssues=true&active=true&includeMergedParties=true": 2} {
		e.must(e.do("GET", "/api/parties?"+query, e.admin, nil, &page), 200, query)
		if page.Total != want {
			t.Fatalf("%s: %d parties, want %d: %+v", query, page.Total, want, page.Items)
		}
	}

	// Corregir es terminar el contacto erróneo (y añadir otro): la incidencia se va con él.
	for _, c := range []papp.ContactResult{mail, phone} {
		id := c.Contacts[len(c.Contacts)-1].ID
		got = papp.PartyDTO{}
		e.must(e.do("POST", contacts+"/"+id+"/end", e.admin, nil, &got), 200, "end a badly formed contact")
	}
	e.must(e.do("GET", "/api/parties?withDataQualityIssues=true", e.admin, nil, &page), 200, "nobody has issues now")
	if len(got.DataQualityIssues) != 0 || page.Total != 0 {
		t.Fatalf("issues cleared: %+v, %d parties still with issues", got.DataQualityIssues, page.Total)
	}
}
