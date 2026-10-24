package host

import (
	"context"
	"errors"
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
	invdomain "github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	modapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
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
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// histories dice a Historial dónde se guarda la historia de cada clase de agregado. Por ahora sólo
// las lee un administrador global: dejar que quien puede ver un agregado lea su historia necesita
// una guarda por clase (la consulta que lo carga), que se añade clase a clase.
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
		h.Accounting.Audit: {accdomain.AccountKind, accdomain.EntryKind, accdomain.LedgerKind, accdomain.ParkedKind},
		h.Assets.Audit:     {astdomain.AssetKind},
		h.Documents.Audit:  {docdomain.DocumentKind},
		h.Shipments.Audit:  {shpdomain.ShipmentKind, shpdomain.CarrierKind},
		h.Work.Audit:       {wrkdomain.WorkKind, wrkdomain.TimeEntryKind},
		h.Financial.Audit:  {findomain.AccountKind, findomain.ProductKind, findomain.AgreementKind},
		h.Exchange.Audit:   {exgdomain.CurrencyKind, exgdomain.SettingsKind, exgdomain.ReservationKind},
		h.Modules.Audit:    {moddomain.FeatureKind, moddomain.ActivationKind},
		h.Exports.Audit:    {expdomain.JobKind},
	} {
		for _, kind := range kinds {
			h.Audit.Register(kind, log, nil)
		}
	}
}

// FinancialInstitutions dice a Financial qué empresas son entidades financieras: en Parties, una
// organización interna que además tiene el rol de entidad financiera, ambos en vigor. Pregunta
// como el anfitrión: responde un hecho sobre una empresa, no muestra nada de ella.
type FinancialInstitutions struct {
	Parties *parties.Module
	system  func() context.Context
}

// IsFinancialInstitution implementa Institutions de Financial.
func (f FinancialInstitutions) IsFinancialInstitution(ctx context.Context, company findomain.OrganizationID) (bool, error) {
	p, err := f.Parties.Service.Get.Handle(authz.WithContext(ctx, authzOf(f.system())), parapp.GetParty{ID: pardomain.PartyID{UUID: company.UUID}})
	if errors.Is(err, fw.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	internal, institution := false, false
	for _, r := range p.Roles {
		if !r.Active {
			continue
		}
		switch r.RoleType {
		case pardomain.RoleInternalOrganization.String():
			internal = true
		case pardomain.RoleFinancialInstitution.String():
			institution = true
		}
	}
	return internal && institution, nil
}

// financialCapability es la funcionalidad de Modules que tiene una entidad financiera.
var financialCapability = modapp.FeatureRef{Kind: moddomain.Capability, Code: "financial"}

// SectorCapabilities dice a Modules lo que tiene una empresa por lo que es: la capacidad
// financiera va con ser entidad financiera, como el sector financiero. Así, lo que muestra la
// interfaz y lo que permite el sector son lo mismo, decidido en un solo sitio: los roles de la
// empresa en Parties.
type SectorCapabilities struct{ Institutions FinancialInstitutions }

// Features implementa Derivation de Modules.
func (SectorCapabilities) Features() []modapp.FeatureRef {
	return []modapp.FeatureRef{financialCapability}
}

// Of implementa Derivation de Modules.
func (s SectorCapabilities) Of(ctx context.Context, organization fw.UUID) ([]modapp.FeatureRef, error) {
	ok, err := s.Institutions.IsFinancialInstitution(ctx, findomain.OrganizationID{UUID: organization})
	if err != nil || !ok {
		return nil, err
	}
	return []modapp.FeatureRef{financialCapability}, nil
}

// Holders implementa Derivation de Modules.
func (s SectorCapabilities) Holders(ctx context.Context, f modapp.FeatureRef) ([]fw.UUID, error) {
	if f != financialCapability {
		return nil, nil
	}
	all, err := s.Institutions.Parties.Organizations.All(ctx)
	if err != nil {
		return nil, err
	}
	out := []fw.UUID{}
	for _, o := range all {
		id, err := fw.ParseUUID(o.ID)
		if err != nil {
			continue
		}
		if ok, err := s.Institutions.IsFinancialInstitution(ctx, findomain.OrganizationID{UUID: id}); err != nil {
			return nil, err
		} else if ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// CustomerAccounts es la lista de las cuentas que una entidad lleva a sus clientes, como
// exportación: lee a través de la búsqueda de Financial, así que el fichero tiene lo que puede ver
// quien lo pidió.
type CustomerAccounts struct{ Financial *financial.Module }

// Key implementa Dataset de exports.
func (CustomerAccounts) Key() string { return "customer-accounts" }

// Title implementa Dataset de exports.
func (CustomerAccounts) Title() string { return "Cuentas de clientes" }

// Permission implementa Dataset de exports.
func (CustomerAccounts) Permission() authz.Permission { return finapp.PermAccountRead }

// Filters implementa Dataset de exports.
func (CustomerAccounts) Filters() []string {
	return []string{"company", "status", "currency", "name", "use", "party", "demo", mergedFilter}
}

// Columns implementa Dataset de exports.
func (CustomerAccounts) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "number", Header: "Número"}, {Field: "name", Header: "Nombre"}, {Field: "currency", Header: "Divisa"},
		{Field: "status", Header: "Estado"}, {Field: "holder", Header: "Titular"}, {Field: "opened", Header: "Apertura", Type: expdomain.Date},
		{Field: "closed", Header: "Cierre", Type: expdomain.Date}, {Field: "virtual", Header: "Virtual", Type: expdomain.Boolean},
		{Field: "demo", Header: "Pruebas", Type: expdomain.Boolean}}
}

// Page implementa Dataset de exports: el cursor es el número de la página.
func (c CustomerAccounts) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	number, _ := strconv.Atoi(cursor)
	if number < 1 {
		number = 1
	}
	page, err := c.Financial.Service.Search.Handle(ctx, finapp.SearchAccounts{Company: filters["company"], Status: filters["status"],
		Currency: filters["currency"], Name: filters["name"], Use: filters["use"], Party: filters["party"], Demo: filters["demo"],
		IncludeMergedParties: filters["includeMergedParties"] == "true", Page: number, Size: limit})
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
	_ findomain.Institutions = FinancialInstitutions{}
	_ modapp.Derivation      = SectorCapabilities{}
	_ expapp.Dataset         = CustomerAccounts{}
)
