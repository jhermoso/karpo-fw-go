//go:build smoke

package smoke

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/host"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Lo que carga -seed: lo justo para recorrer el ciclo de venta de punta a punta sin preparar nada
// a mano (docs/SMOKE-MANUAL.md). Todo entra por las rutas del anfitrión y como el perfil admin,
// como lo daría de alta una persona: se comprueba lo mismo que comprobaría con ella.

// taxIDDocument es el tipo de documento «número de identificación fiscal» (TXID) de Parties.
const taxIDDocument = "c0000000-0004-0000-0000-000000000003"

// Lo sembrado que nombran la guía y la prueba. Los NIF son inventados, con su dígito de control
// válido.
const (
	CompanyName   = "Humo Distribuciones, S.L."
	CompanyTaxID  = "B98765431"
	CustomerName  = "Cliente de Humo, S.L."
	CustomerTaxID = "B12345674"
	ProductSKU    = "HUMO-01"
	ProductName   = "Caja de humo"
	ProductPrice  = "12.50"
	ProductTax    = "G21"
	WarehouseCode = "AL1"
	WarehouseName = "Almacén central"
	StockUnits    = "100"
	SeriesCode    = "FA"
	TermsCode     = "30D"
	TermsDays     = 30
)

// Seeded es lo que cargó la semilla: los identificadores que pide la guía del recorrido.
type Seeded struct {
	Company   string // la empresa interna que vende
	Customer  string // su cliente
	Product   string // un producto vendible, con precio y existencias
	Warehouse string // el almacén de las existencias
	Series    string // la serie de facturación del año en curso
	Terms     string // las condiciones de pago del cliente
}

// Lines son las asignaciones de PowerShell que copian los identificadores en variables, como las
// usa la guía.
func (s Seeded) Lines() []string {
	return []string{
		"$company   = '" + s.Company + "'   # " + CompanyName + " (empresa interna, NIF " + CompanyTaxID + ")",
		"$customer  = '" + s.Customer + "'   # " + CustomerName + " (cliente, NIF " + CustomerTaxID + ")",
		"$product   = '" + s.Product + "'   # " + ProductSKU + " " + ProductName + ", " + ProductPrice + " + IVA " + ProductTax,
		"$warehouse = '" + s.Warehouse + "'   # " + WarehouseCode + " " + WarehouseName + ", " + StockUnits + " unidades",
		"$series    = '" + s.Series + "'   # serie " + SeriesCode + " del año en curso",
		"$terms     = '" + s.Terms + "'   # " + TermsCode + ": un vencimiento a " + strconv.Itoa(TermsDays) + " días",
	}
}

// created es la respuesta de un alta: basta su identificador.
type created struct {
	ID string `json:"id"`
}

// seeder envía las altas con una sesión y se queda con el primer fallo.
type seeder struct {
	p     *Publisher
	token string
	err   error
}

// send da de alta algo y devuelve su identificador. want es el estado que debe responder.
func (s *seeder) send(method, path string, want int, body any) string {
	if s.err != nil {
		return ""
	}
	var out created
	if status, raw := s.p.call(s.p, method, path, s.token, body, &out); status != want {
		s.err = fmt.Errorf("smoke: seed: %s %s: %d, want %d: %s", method, path, status, want, raw)
	}
	return out.ID
}

// Seed carga, si aún no hay ninguna party, lo que necesita el recorrido de la guía:
//
//   - una empresa interna con NIF y domicilio fiscal, contribuyente en territorio común;
//   - un cliente suyo, con NIF y domicilio;
//   - los tipos de IVA general, reducido y superreducido;
//   - un producto vendible con precio y un almacén con existencias;
//   - una serie de facturación del año en curso;
//   - unas condiciones de pago a 30 días, que son las del cliente;
//   - un plan de cuentas mínimo y el libro de la empresa, para que las facturas y los cobros se
//     contabilicen.
//
// Si ya hay alguna party no carga nada y lo dice (Seeded vacío).
func (p *Publisher) Seed(ctx context.Context) (Seeded, string, error) {
	admin, _ := profile("admin")
	session, err := p.Open(admin)
	if err != nil {
		return Seeded{}, "", err
	}
	var page struct {
		Total int64 `json:"total"`
	}
	if status, body := p.call(p, "GET", "/api/parties?size=1", session.AccessToken, nil, &page); status != http.StatusOK {
		return Seeded{}, "", fmt.Errorf("smoke: GET /api/parties: %d %s", status, body)
	}
	if page.Total > 0 {
		return Seeded{}, "there is data already: nothing was loaded", nil
	}
	// «Hoy» es el de la empresa, como lo toma Billing al emitir: en España, en la zona de Madrid. Con
	// el de UTC, la serie de la primera hora del 1 de enero sería la del año anterior.
	zone, err := (&host.CompanyZones{Geography: p.Host.Geography}).Location(ctx, "", "ES")
	if err != nil {
		return Seeded{}, "", fmt.Errorf("smoke: seed: %w", err)
	}
	today := fw.Now().In(zone)
	year := today.Year()

	s := &seeder{p: p, token: session.AccessToken}
	var out Seeded

	// Parties: la empresa y su cliente, cada uno con su NIF y su domicilio fiscal.
	organization := func(name, taxID string, roles []string, address map[string]any) string {
		id := s.send("POST", "/api/organizations", http.StatusCreated, map[string]any{"legalName": name, "roles": roles})
		s.send("POST", "/api/parties/"+id+"/identifications", http.StatusOK,
			map[string]any{"documentType": taxIDDocument, "country": "ES", "number": taxID, "primary": true})
		s.send("POST", "/api/parties/"+id+"/contacts", http.StatusOK,
			map[string]any{"kind": "postal", "address": address, "purposes": []string{"billing", "default"}})
		return id
	}
	out.Company = organization(CompanyName, CompanyTaxID, []string{pardomain.RoleInternalOrganization.String()},
		map[string]any{"streetType": "Calle", "line1": "Mayor", "number": "1", "postalCode": "28013", "locality": "Madrid", "region": "Madrid", "country": "ES"})
	out.Customer = organization(CustomerName, CustomerTaxID, nil,
		map[string]any{"streetType": "Calle", "line1": "Aragó", "number": "200", "postalCode": "08011", "locality": "Barcelona", "region": "Barcelona", "country": "ES"})
	s.send("POST", "/api/party-relationships", http.StatusCreated,
		map[string]any{"type": pardomain.RelCustomer.String(), "fromParty": out.Customer, "toParty": out.Company})

	// Fiscal: los tipos de IVA de territorio común y la empresa como contribuyente.
	for _, r := range []map[string]any{
		{"code": "G21", "description": "General", "rate": "21", "surcharge": "5.2"},
		{"code": "R10", "description": "Reducido", "rate": "10", "surcharge": "1.4"},
		{"code": "SR4", "description": "Superreducido", "rate": "4", "surcharge": "0.5"},
	} {
		r["type"], r["territory"], r["from"] = "vat", "common", "2012-09-01"
		s.send("POST", "/api/fiscal/tax-rates", http.StatusCreated, r)
	}
	s.send("POST", "/api/fiscal/taxpayers", http.StatusCreated, map[string]any{"organization": out.Company, "territory": "common", "fiscalYearStartMonth": 1})

	// Products e Inventory: un producto que se vende y se almacena, y existencias para servirlo.
	out.Product = s.send("POST", "/api/products/products", http.StatusCreated, map[string]any{"company": out.Company, "sku": ProductSKU,
		"name": ProductName, "kind": "good", "uom": "ea", "taxCode": ProductTax, "basePrice": ProductPrice, "forSale": true, "stocked": true})
	out.Warehouse = s.send("POST", "/api/inventory/warehouses", http.StatusCreated, map[string]any{"company": out.Company, "code": WarehouseCode, "name": WarehouseName})
	s.send("POST", "/api/inventory/receipts", http.StatusCreated, map[string]any{"warehouse": out.Warehouse, "product": out.Product, "quantity": StockUnits,
		"unitCost": "6", "date": today.Format("2006-01-02")})

	// Billing: la serie del año.
	out.Series = s.send("POST", "/api/billing/series", http.StatusCreated, map[string]any{"seller": out.Company, "code": SeriesCode, "year": year})

	// Receivables: 30 días, las condiciones del cliente, sin límite de crédito.
	out.Terms = s.send("POST", "/api/receivables/terms", http.StatusCreated, map[string]any{"seller": out.Company, "code": TermsCode,
		"description": strconv.Itoa(TermsDays) + " días", "installments": 1, "daysToFirst": TermsDays})
	s.send("PUT", "/api/receivables/credit", http.StatusOK, map[string]any{"seller": out.Company, "customer": out.Customer, "terms": out.Terms})

	// Accounting: las cuentas que usan una factura y un cobro, y el libro con su perfil.
	chart := [][2]string{{"4300", "Clientes"}, {"4312", "Efectos comerciales en gestión de cobro"}, {"4770", "Hacienda Pública, IVA repercutido"},
		{"4775", "Recargo de equivalencia"}, {"5700", "Caja"}, {"5720", "Bancos"}, {"7000", "Ventas de mercaderías"}}
	for _, a := range chart {
		s.send("POST", "/api/accounting/accounts", http.StatusCreated, map[string]any{"company": out.Company, "code": a[0], "name": a[1], "postable": true})
	}
	s.send("POST", "/api/accounting/ledgers", http.StatusCreated, map[string]any{"company": out.Company, "startMonth": 1,
		"accounts": map[string]string{"customers": "4300", "revenue": "7000", "surcharge": "4775", "cash": "5700", "bank": "5720", "direct-debit-clearing": "4312"},
		"taxCodes": map[string]string{"G21": "4770", "R10": "4770", "SR4": "4770"}})
	if s.err != nil {
		return Seeded{}, "", s.err
	}

	// Lo que publicaron las altas llega a quienes lo escuchan antes de abrir la puerta.
	if _, err := p.Host.Deliver(ctx); err != nil {
		return Seeded{}, "", fmt.Errorf("smoke: seed: delivering: %w", err)
	}
	said := []string{"loaded a company and its customer, the Spanish VAT rates, a product with " +
		"stock in a warehouse, the invoice series " + SeriesCode + " " + strconv.Itoa(year) + ", " + strconv.Itoa(TermsDays) +
		"-day payment terms and a minimal chart of accounts with its ledger.",
		"Paste these lines into PowerShell (docs/SMOKE-MANUAL.md):"}
	return out, strings.Join(append(said, out.Lines()...), "\n"), nil
}
