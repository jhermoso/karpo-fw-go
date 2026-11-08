package parties_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// El perfil de la empresa interna (D-133) por HTTP, en memoria y en SQLite: se lee con el permiso
// de leer parties y se cambia con el de modificarlas, en el ámbito de la empresa; una zona que no
// es IANA da 422 Company.InvalidTimeZone, y una vacía la limpia.
func TestParties_InternalOrganizationProfile(t *testing.T) {
	for name, useSQL := range map[string]bool{"memory": false, "sqlite": true} {
		t.Run(name, func(t *testing.T) {
			e := compose(t, newMemoryDirectory)
			if useSQL {
				ctx := context.Background()
				raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "profile.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
			e.profile()
			e.profileSettings()
			e.profilePricing()
		})
	}
}

func (e *env) profile() {
	e.t.Helper()
	var acme, globex, pedro papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme Perfil",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &acme), 201, "register acme")
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Globex Perfil",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &globex), 201, "register globex")
	e.grant("clerk", authz.Full, acme.ID)
	e.grant("viewer", authz.ReadOnly, acme.ID)
	e.grant("reader", authz.Full, acme.ID)
	e.grant("outsider", authz.Full, globex.ID)
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Perfil",
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &pedro), 201, "a customer of acme")

	path := "/api/profiles/internal-organizations/" + acme.ID
	var got papp.InternalOrganizationProfileDTO
	e.must(e.do("GET", path, e.viewer, nil, &got), 200, "a profile not saved yet")
	if got.InternalOrganizationID != acme.ID || got.TimeZone != nil {
		e.badProfile(got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": "Atlantic/Canary"}, &got), 200, "the clerk sets the zone")
	if got.TimeZone == nil || *got.TimeZone != "Atlantic/Canary" {
		e.badProfile(got)
	}
	e.must(e.do("GET", path, e.reader, nil, &got), 200, "read it back")
	if got.TimeZone == nil || *got.TimeZone != "Atlantic/Canary" {
		e.badProfile(got)
	}
	if tz, err := e.mod.CompanyTimeZones.TimeZone(context.Background(), acme.ID); err != nil || tz != "Atlantic/Canary" {
		e.t.Fatalf("the zone other contexts read: %q %v", tz, err)
	}

	e.mustProblem(e.problem("PUT", path, e.clerk, map[string]any{"timeZone": "Romance Standard Time"}), 422, domain.ErrInvalidTimeZone,
		"a Windows name is not an IANA code")
	e.mustProblem(e.problem("PUT", path, e.clerk, map[string]any{"timeZone": "Mars/Olympus"}), 422, domain.ErrInvalidTimeZone,
		"an unknown zone")
	e.must(e.do("PUT", path, e.viewer, map[string]any{"timeZone": "Europe/Madrid"}, nil), 403, "a read-only grant cannot change it")
	e.must(e.do("PUT", path, e.reader, map[string]any{"timeZone": "Europe/Madrid"}, nil), 403, "nor can who lacks the update permission")
	e.must(e.do("GET", path, e.outsider, nil, nil), 404, "another company does not see it")
	e.must(e.do("PUT", path, e.outsider, map[string]any{"timeZone": "Europe/Madrid"}, nil), 404, "nor change it")
	e.must(e.do("GET", path, "", nil, nil), 401, "anonymous callers do not")
	e.must(e.do("GET", "/api/profiles/internal-organizations/"+pedro.ID, e.clerk, nil, nil), 404, "a party that is not an internal organization")
	e.must(e.do("PUT", "/api/profiles/internal-organizations/"+globex.ID, e.admin, map[string]any{"timeZone": "UTC"}, nil), 200, "the global admin")

	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": nil}, &got), 200, "null clears it")
	if got.TimeZone != nil {
		e.badProfile(got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": "Atlantic/Canary"}, nil), 200, "set it again")
	e.must(e.do("PUT", path, e.clerk, map[string]any{}, &got), 200, "an empty body clears it: the PUT replaces the profile")
	if got.TimeZone != nil {
		e.badProfile(got)
	}
	trail, err := e.mod.Audit.Trail(context.Background(), domain.InternalOrganizationProfileKind, acme.ID)
	if err != nil || len(trail) != 4 || trail[0].Operation != "created" || trail[3].Operation != "updated" {
		e.t.Fatalf("audit of the profile: %+v %v", trail, err)
	}
}

func (e *env) badProfile(got papp.InternalOrganizationProfileDTO) {
	e.t.Helper()
	tz := "<nil>"
	if got.TimeZone != nil {
		tz = *got.TimeZone
	}
	e.t.Fatalf("profile: %s %s", got.InternalOrganizationID, tz)
}

// Los ajustes de precios de D-148 en el mismo perfil, de ida y vuelta: la tarifa general (un id, que
// el puerto de Products comprueba cuando cambia) y el precio automático (nulo es sí).
func (e *env) profilePricing() {
	e.t.Helper()
	var acme papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme Precios",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &acme), 201, "register acme")
	e.grant("clerk", authz.Full, acme.ID)
	path := "/api/profiles/internal-organizations/" + acme.ID
	var got papp.InternalOrganizationProfileDTO
	e.must(e.do("GET", path, e.clerk, nil, &got), 200, "nothing saved yet")
	if got.DefaultPriceList != nil || !got.AutomaticPricing {
		e.t.Fatalf("no general list, automatic pricing by default: %+v", got)
	}
	// Para los demás contextos, el valor cero es el automático: sin perfil y con un id que no lo es.
	for _, id := range []string{acme.ID, "not-an-id"} {
		if st, err := e.mod.CompanyProfiles.Settings(context.Background(), id); err != nil || st.ManualPricing {
			e.t.Fatalf("automatic pricing without a profile (%s): %+v %v", id, st, err)
		}
	}
	list := "8f0c1a52-6d4e-4b8a-9f3e-2a7b5c1d0e94"
	e.must(e.do("PUT", path, e.clerk, map[string]any{"defaultPriceList": list, "automaticPricing": false}, &got), 200, "set them")
	e.must(e.do("GET", path, e.clerk, nil, &got), 200, "read them back")
	if got.DefaultPriceList == nil || *got.DefaultPriceList != list || got.AutomaticPricing {
		e.t.Fatalf("the pricing settings: %+v", got)
	}
	st, err := e.mod.CompanyProfiles.Settings(context.Background(), acme.ID)
	if err != nil || st.DefaultPriceList != list || !st.ManualPricing {
		e.t.Fatalf("the settings other contexts read: %+v %v", st, err)
	}
	e.mustProblem(e.problem("PUT", path, e.clerk, map[string]any{"defaultPriceList": "mayor"}), 422, domain.ErrInvalidDefaultPriceList, "not an id")
	unusable := "3b2d9e71-0c4f-4a6e-8d15-7f9a2c4b6e08"
	e.priceLists.ids.Store(unusable, true)
	e.mustProblem(e.problem("PUT", path, e.clerk, map[string]any{"defaultPriceList": unusable}), 422, domain.ErrUnknownDefaultPriceList,
		"a price list that does not exist, is another company's or is retired")
	// La que ya estaba no se vuelve a mirar: retirarla después no impide cambiar el resto.
	e.priceLists.ids.Store(list, true)
	e.must(e.do("PUT", path, e.clerk, map[string]any{"defaultPriceList": list, "automaticPricing": false, "timeZone": "Europe/Madrid"}, nil), 200,
		"the saved list is not checked again")
	e.priceLists.ids.Delete(list)
	got = papp.InternalOrganizationProfileDTO{} // defaultPriceList se omite cuando no hay
	e.must(e.do("PUT", path, e.clerk, map[string]any{"automaticPricing": true}, &got), 200, "explicitly on")
	if got.DefaultPriceList != nil || !got.AutomaticPricing {
		e.t.Fatalf("the PUT replaces the whole profile: %+v", got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{}, &got), 200, "cleared")
	if st, err := e.mod.CompanyProfiles.Settings(context.Background(), acme.ID); err != nil || st.DefaultPriceList != "" || st.ManualPricing {
		e.t.Fatalf("cleared settings: %+v %v", st, err)
	}
}

// Los ajustes de facturación de D-144 en el mismo perfil, y el patrón de cada tipo de código en
// su propio recurso: mismos permisos y ámbito que la zona; un valor fuera del enumerado o del rango,
// o un patrón que no compila, da 422 con su código; null limpia.
func (e *env) profileSettings() {
	e.t.Helper()
	var acme, globex papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme Ajustes",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &acme), 201, "register acme")
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Globex Ajustes",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &globex), 201, "register globex")
	e.grant("clerk", authz.Full, acme.ID)
	e.grant("viewer", authz.ReadOnly, acme.ID)
	e.grant("reader", authz.Full, acme.ID)
	e.grant("outsider", authz.Full, globex.ID)

	path := "/api/profiles/internal-organizations/" + acme.ID
	var got papp.InternalOrganizationProfileDTO
	e.must(e.do("GET", path, e.viewer, nil, &got), 200, "nothing saved yet")
	if got.LineRounding != nil || got.PriceDecimals != nil || got.InstallmentsTiming != nil {
		e.t.Fatalf("an empty profile: %+v", got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": "Europe/Madrid", "lineRounding": "gross_then_discount", "priceDecimals": 2,
		"installmentsTiming": "with_issue"}, &got), 200, "the clerk sets them")
	e.must(e.do("GET", path, e.reader, nil, &got), 200, "read them back")
	if got.TimeZone == nil || *got.TimeZone != "Europe/Madrid" || got.LineRounding == nil || *got.LineRounding != "gross_then_discount" ||
		got.PriceDecimals == nil || *got.PriceDecimals != 2 || got.InstallmentsTiming == nil || *got.InstallmentsTiming != "with_issue" {
		e.t.Fatalf("the settings: %+v", got)
	}
	st, err := e.mod.CompanyProfiles.Settings(context.Background(), acme.ID)
	if err != nil || st.LineRounding != "gross_then_discount" || st.PriceDecimals == nil || *st.PriceDecimals != 2 || st.InstallmentsTiming != "with_issue" {
		e.t.Fatalf("the settings other contexts read: %+v %v", st, err)
	}
	for _, c := range []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"lineRounding": "NetOnce"}, domain.ErrInvalidLineRounding},
		{map[string]any{"priceDecimals": 7}, domain.ErrInvalidPriceDecimals},
		{map[string]any{"priceDecimals": -1}, domain.ErrInvalidPriceDecimals},
		{map[string]any{"installmentsTiming": "InIssueTransaction"}, domain.ErrInvalidInstallmentsTiming},
	} {
		e.mustProblem(e.problem("PUT", path, e.clerk, c.body), 422, c.code, fmt.Sprint(c.body))
	}
	e.must(e.do("PUT", path, e.viewer, map[string]any{"lineRounding": "once"}, nil), 403, "a read-only grant cannot change them")
	e.must(e.do("PUT", path, e.reader, map[string]any{"lineRounding": "once"}, nil), 403, "nor can who lacks the update permission")
	e.must(e.do("PUT", path, e.outsider, map[string]any{"lineRounding": "once"}, nil), 404, "nor another company")
	e.must(e.do("PUT", path, e.clerk, map[string]any{"lineRounding": "once", "priceDecimals": 0}, &got), 200, "zero decimals")
	if got.PriceDecimals == nil || *got.PriceDecimals != 0 || got.InstallmentsTiming != nil || got.TimeZone != nil {
		e.t.Fatalf("the PUT replaces the whole profile: %+v", got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{"lineRounding": nil, "priceDecimals": nil}, &got), 200, "null clears them")
	if got.LineRounding != nil || got.PriceDecimals != nil {
		e.t.Fatalf("cleared: %+v", got)
	}

	// El patrón de las series de facturación.
	pattern := path + "/code-patterns/invoice_series"
	var cp papp.CodePatternDTO
	e.must(e.do("GET", pattern, e.viewer, nil, &cp), 200, "no pattern yet")
	if cp.Pattern != nil || cp.CodeKind != "invoice_series" || cp.InternalOrganizationID != acme.ID {
		e.t.Fatalf("no pattern: %+v", cp)
	}
	e.must(e.do("PUT", pattern, e.clerk, map[string]any{"pattern": "F[A-Z]/[0-9]{2}"}, &cp), 200, "the clerk sets it")
	e.must(e.do("GET", pattern, e.reader, nil, &cp), 200, "read it back")
	if cp.Pattern == nil || *cp.Pattern != "F[A-Z]/[0-9]{2}" {
		e.t.Fatalf("the pattern: %+v", cp)
	}
	if p, err := e.mod.CompanyProfiles.CodePattern(context.Background(), acme.ID, "invoice_series"); err != nil || p != "F[A-Z]/[0-9]{2}" {
		e.t.Fatalf("the pattern other contexts read: %q %v", p, err)
	}
	e.mustProblem(e.problem("PUT", pattern, e.clerk, map[string]any{"pattern": "F[A-Z"}), 422, domain.ErrInvalidCodePattern, "it does not compile")
	e.mustProblem(e.problem("PUT", pattern, e.clerk, map[string]any{"pattern": strings.Repeat("A", 201)}), 422, domain.ErrInvalidCodePattern, "too long")
	e.mustProblem(e.problem("PUT", path+"/code-patterns/customer_number", e.clerk, map[string]any{"pattern": "C[0-9]+"}), 422, domain.ErrInvalidCodeKind,
		"an unknown code kind")
	e.mustProblem(e.problem("GET", path+"/code-patterns/customer_number", e.clerk, nil), 422, domain.ErrInvalidCodeKind, "nor read")
	e.must(e.do("PUT", pattern, e.viewer, map[string]any{"pattern": "X"}, nil), 403, "a read-only grant cannot change it")
	e.must(e.do("PUT", pattern, e.reader, map[string]any{"pattern": "X"}, nil), 403, "nor can who lacks the update permission")
	e.must(e.do("GET", pattern, e.outsider, nil, nil), 404, "another company does not see it")
	e.must(e.do("PUT", pattern, e.outsider, map[string]any{"pattern": "X"}, nil), 404, "nor change it")
	e.must(e.do("GET", pattern, "", nil, nil), 401, "anonymous callers do not")
	e.must(e.do("PUT", pattern, e.clerk, map[string]any{"pattern": nil}, &cp), 200, "null clears it")
	if cp.Pattern != nil {
		e.t.Fatalf("cleared: %+v", cp)
	}
	trail, err := e.mod.Audit.Trail(context.Background(), domain.InternalOrganizationProfileKind, acme.ID)
	if err != nil || len(trail) != 5 {
		e.t.Fatalf("audit of the settings and the pattern: %+v %v", trail, err)
	}
}
