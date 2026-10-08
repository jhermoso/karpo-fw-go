package host

import (
	"context"
	"strconv"

	accdomain "github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	astdomain "github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	bildomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	docdomain "github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
	exgdomain "github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	facdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	findomain "github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	fisdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	hrdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	impdomain "github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	invdomain "github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	moddomain "github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	orddomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	paydomain "github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	prldomain "github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	prodomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	purdomain "github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	recdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	secdomain "github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	shpdomain "github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	tredomain "github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	wrkdomain "github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// histories tells Historial where the history of each kind of aggregate is kept. For now only a
// global administrator reads them: letting whoever can see an aggregate read its history needs a
// guard per kind (the query that loads it), which is added kind by kind.
func (h *Host) histories() {
	for log, kinds := range map[application.AuditLog][]string{
		h.Parties.Audit:     {pardomain.PartyKind, pardomain.RelationshipKind},
		h.Facilities.Audit:  {facdomain.FacilityKind},
		h.Security.Audit:    {secdomain.UserKind, secdomain.RoleKind},
		h.Products.Audit:    {prodomain.ProductKind, prodomain.CategoryKind, prodomain.PriceListKind},
		h.Fiscal.Audit:      {fisdomain.TaxRateKind, fisdomain.WithholdingKind, fisdomain.FilingKind, fisdomain.TaxpayerKind},
		h.HR.Audit:          {hrdomain.EmploymentKind, hrdomain.PositionKind, hrdomain.WorkCenterKind},
		h.Payroll.Audit:     {prldomain.PayslipKind, prldomain.ProfileKind, prldomain.EmployerAccountKind},
		h.Receivables.Audit: {recdomain.ReceivableKind, recdomain.CollectionKind, recdomain.CreditProfileKind, recdomain.TermsKind},
		h.Inventory.Audit:   {invdomain.WarehouseKind, invdomain.MovementKind},
		h.Orders.Audit:      {orddomain.OrderKind, orddomain.DeliveryKind, orddomain.TermsKind, orddomain.QuoteKind},
		h.Billing.Audit:     {bildomain.InvoiceKind, bildomain.SeriesKind},
		h.Purchases.Audit:   {purdomain.InvoiceKind, purdomain.SupplierKind},
		h.Payments.Audit:    {paydomain.PayableKind, paydomain.PaymentKind},
		h.Treasury.Audit: {tredomain.AccountKind, tredomain.MandateKind, tredomain.RemittanceKind, tredomain.TransferOrderKind,
			tredomain.StatementKind},
		h.Accounting.Audit: {accdomain.AccountKind, accdomain.EntryKind, accdomain.LedgerKind},
		h.Assets.Audit:     {astdomain.AssetKind},
		h.Documents.Audit:  {docdomain.DocumentKind},
		h.Shipments.Audit:  {shpdomain.ShipmentKind, shpdomain.CarrierKind},
		h.Work.Audit:       {wrkdomain.WorkKind, wrkdomain.TimeEntryKind},
		h.Financial.Audit:  {findomain.AccountKind},
		h.Exchange.Audit:   {exgdomain.CurrencyKind, exgdomain.SettingsKind, exgdomain.ReservationKind},
		h.Modules.Audit:    {moddomain.FeatureKind, moddomain.ActivationKind},
		h.Imports.Audit:    {impdomain.RunKind, impdomain.ReferenceKind},
		h.Exports.Audit:    {expdomain.JobKind},
	} {
		for _, kind := range kinds {
			h.Audit.Register(kind, log, nil)
		}
	}
}

// LegalEntities loads the companies an import brings (records of kind legal-entity) into Parties,
// with its own use cases: who imports needs the permission to register an organization.
type LegalEntities struct{ Parties *parties.Module }

// Kind implements imports' Loader.
func (LegalEntities) Kind() string { return impdomain.KindLegalEntity }

// EntityType implements imports' Loader.
func (LegalEntities) EntityType() string { return pardomain.PartyKind }

// Find looks for an internal organization of that name, without minding case or accents.
func (l LegalEntities) Find(ctx context.Context, r impdomain.Record, _ impdomain.Refs) (string, error) {
	all, err := l.Parties.Organizations.All(ctx)
	if err != nil {
		return "", err
	}
	name := impdomain.Fold(r.Fields["name"])
	for _, o := range all {
		if impdomain.Fold(o.Name) == name {
			return o.ID, nil
		}
	}
	return "", nil
}

// Apply registers the company when it does not exist. One that exists is left as it is: an
// import does not rename a company.
func (l LegalEntities) Apply(ctx context.Context, r impdomain.Record, existing string, _ impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	p, err := l.Parties.Service.RegisterOrganization.Handle(ctx, parapp.RegisterOrganization{LegalName: r.Fields["name"],
		Roles: []string{pardomain.RoleInternalOrganization.String()}})
	if err != nil {
		return "", "", err
	}
	return p.ID, impdomain.Created, nil
}

// CustomerAccounts is the list of the accounts an institution keeps for its customers, as an
// export: it reads through the search of Financial, so the file has what who asked may see.
type CustomerAccounts struct{ Financial *financial.Module }

// Key implements exports' Dataset.
func (CustomerAccounts) Key() string { return "customer-accounts" }

// Title implements exports' Dataset.
func (CustomerAccounts) Title() string { return "Cuentas de clientes" }

// Permission implements exports' Dataset.
func (CustomerAccounts) Permission() authz.Permission { return finapp.PermAccountRead }

// Filters implements exports' Dataset.
func (CustomerAccounts) Filters() []string {
	return []string{"company", "status", "currency", "name", "use", "party", "demo"}
}

// Columns implements exports' Dataset.
func (CustomerAccounts) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "number", Header: "Número"}, {Field: "name", Header: "Nombre"}, {Field: "currency", Header: "Divisa"},
		{Field: "status", Header: "Estado"}, {Field: "holder", Header: "Titular"}, {Field: "opened", Header: "Apertura", Type: expdomain.Date},
		{Field: "closed", Header: "Cierre", Type: expdomain.Date}, {Field: "virtual", Header: "Virtual", Type: expdomain.Boolean},
		{Field: "demo", Header: "Pruebas", Type: expdomain.Boolean}}
}

// Page implements exports' Dataset: the cursor is the number of the page.
func (c CustomerAccounts) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	number, _ := strconv.Atoi(cursor)
	if number < 1 {
		number = 1
	}
	page, err := c.Financial.Service.Search.Handle(ctx, finapp.SearchAccounts{Company: filters["company"], Status: filters["status"],
		Currency: filters["currency"], Name: filters["name"], Use: filters["use"], Party: filters["party"], Demo: filters["demo"], Page: number, Size: limit})
	if err != nil {
		return nil, "", err
	}
	rows := make([]expdomain.Row, 0, len(page.Items))
	for _, a := range page.Items {
		rows = append(rows, expdomain.Row{"number": a.Number, "name": a.Name, "currency": a.Currency, "status": a.Status, "holder": a.Holder,
			"opened": a.Opened, "closed": a.Closed, "virtual": a.Virtual, "demo": a.Demo})
	}
	if page.Number >= page.TotalPages {
		return rows, "", nil
	}
	return rows, strconv.Itoa(page.Number + 1), nil
}

var (
	_ impdomain.Loader = LegalEntities{}
	_ expapp.Dataset   = CustomerAccounts{}
)
