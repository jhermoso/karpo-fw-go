package financial_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de cuentas y de acuerdos dejan fuera por defecto los de parties fusionadas en otra,
// y los traen con includeMergedParties; el registro pedido por su identidad se sigue encontrando
// (docs/DECISIONES-COMUNES.md, D-100). Una cuenta queda fuera sólo si todos sus titulares
// vigentes están fusionados: con uno que no lo esté se queda, y uno que ya dejó de serlo no cuenta.
// Se comprueba en memoria y en SQLite, que traduce la condición sobre los titulares a SQL.
func TestListingsLeaveOutMergedParties_MemoryThenSQLite(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, financial.WithMergedParties(merged))
	leaveOutMerged(h, merged)

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{finfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	leaveOutMerged(h, merged)
}

func leaveOutMerged(h *host, merged *mergedParties) {
	t := h.t
	acme := fw.NewUUID().String()
	h.grant("opener", acme)
	h.grant("viewer", acme)
	h.banks[domain.OrganizationID{UUID: fw.MustParseUUID(acme)}] = true
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	// Cuentas: de ana, de bea, de las dos, una que fue de ana y ahora sólo es de bea, y una de bea
	// en la que ana sólo está autorizada (un autorizado no es la ficha de la cuenta).
	open := func(number, holder string) fapp.AccountDTO {
		t.Helper()
		var acc fapp.AccountDTO
		h.must(h.do("POST", "/api/financial/accounts", "opener", map[string]any{"company": acme, "number": number, "virtual": true, "holder": holder}, &acc), 201, number)
		return acc
	}
	path := func(a fapp.AccountDTO, action string) string { return "/api/financial/accounts/" + a.ID + "/" + action }
	open("virt-ana-01", ana)
	beas := open("virt-bea-01", bea)
	both := open("virt-both-1", bea)
	h.must(h.do("POST", path(both, "holders"), "opener", map[string]any{"party": ana, "role": "holder"}, nil), 200, "ana also holds it")
	left := open("virt-left-1", ana)
	h.must(h.do("POST", path(left, "holders"), "opener", map[string]any{"party": bea, "role": "holder"}, nil), 200, "bea also holds it")
	h.must(h.do("POST", path(left, "holders/primary"), "opener", map[string]any{"party": bea}, nil), 200, "filed under bea")
	h.must(h.do("POST", path(left, "holders/end"), "opener", map[string]any{"party": ana, "role": "holder"}, nil), 200, "ana leaves")
	authorized := open("virt-auth-1", bea)
	h.must(h.do("POST", path(authorized, "holders"), "opener", map[string]any{"party": ana, "role": "authorized"}, nil), 200, "ana is authorized")

	numbers := func(query string) []string {
		t.Helper()
		var page fw.Page[fapp.AccountDTO]
		h.must(h.do("GET", "/api/financial/accounts?company="+acme+query, "viewer", nil, &page), 200, "accounts"+query)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Number)
		}
		slices.Sort(out)
		return out
	}
	if got := numbers(""); !slices.Equal(got, []string{"VIRT-ANA-01", "VIRT-BOTH-1"}) {
		t.Fatalf("accounts: %v", got)
	}
	if got := numbers("&includeMergedParties=true"); len(got) != 5 {
		t.Fatalf("accounts with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/financial/accounts/"+beas.ID, "viewer", nil, nil), 200, "the account of a merged party, by id")

	// Acuerdos: uno con cada cliente.
	ids := map[string]string{}
	for i, c := range []string{ana, bea} {
		var a fapp.AgreementDTO
		h.must(h.do("POST", "/api/financial/agreements", "opener", map[string]any{"company": acme, "customer": c, "number": "CTR-" + string(rune('A'+i)),
			"name": "Contrato", "family": "payment"}, &a), 201, "agreement")
		ids[c] = a.ID
	}
	customers := func(query string) []string {
		t.Helper()
		var page fw.Page[fapp.AgreementDTO]
		h.must(h.do("GET", "/api/financial/agreements?company="+acme+query, "viewer", nil, &page), 200, "agreements"+query)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Customer)
		}
		return out
	}
	if got := customers(""); len(got) != 1 || got[0] != ana {
		t.Fatalf("agreements: %v, want only %s", got, ana)
	}
	if got := customers("&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("agreements with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/financial/agreements/"+ids[bea], "viewer", nil, nil), 200, "the agreement of a merged party, by id")
}
