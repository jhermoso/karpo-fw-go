// Package host is the composition root of Karpo: it builds every bounded context on one backend,
// gives each the ports it asks of the others, carries the Published Language between them,
// declares their permissions to Security, serves their routes behind authentication and runs what
// they leave for a scheduler. Nothing here is business: it is wiring, and the only place that
// knows every context.
package host

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	accinfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	astapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	astinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/audit"
	audapp "github.com/jhermoso/karpo-fw-go/contexts/audit/application"
	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bilinfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/documents"
	docapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	docinfra "github.com/jhermoso/karpo-fw-go/contexts/documents/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/exchange"
	exgapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	exginfra "github.com/jhermoso/karpo-fw-go/contexts/exchange/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/exports"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	facapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	facinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	fininfra "github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fisinfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	geoapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hrinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/imports"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	impdomain "github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	impinfra "github.com/jhermoso/karpo-fw-go/contexts/imports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/inventory"
	invapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	invinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/modules"
	modapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	modinfra "github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	ordapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	ordinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	parinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	payapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	payinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll"
	prlapp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	prlinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	proapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	proinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	purapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	purinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	recinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	secdist "github.com/jhermoso/karpo-fw-go/contexts/security/distribution"
	secinfra "github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	shpapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	shpinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	treapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	treinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/work"
	wrkapp "github.com/jhermoso/karpo-fw-go/contexts/work/application"
	wrkinfra "github.com/jhermoso/karpo-fw-go/contexts/work/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host/mailbox"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Options is what the deployment decides.
type Options struct {
	// JWTSecret signs the sessions; it is required.
	JWTSecret []byte
	// Files keeps the files of the exports (nil: in memory, lost on a restart).
	Files expapp.Files
	// ServicePrincipals are the processes that call the API with their own token.
	ServicePrincipals []authz.ServicePrincipal
	// ApiscoreEntity names, as Parties does, the financial institution whose accounts the files of
	// Apiscore hold: they do not say it themselves. Empty: an import of Apiscore is refused.
	ApiscoreEntity string
}

// Host is Karpo composed.
type Host struct {
	Switch *hotswap.Switch
	Broker *inprocess.Broker

	Geography   *geography.Module
	Facilities  *facilities.Module
	Parties     *parties.Module
	Security    *security.Module
	Products    *products.Module
	Fiscal      *fiscal.Module
	HR          *hr.Module
	Payroll     *payroll.Module
	Receivables *receivables.Module
	Inventory   *inventory.Module
	Orders      *orders.Module
	Billing     *billing.Module
	Purchases   *purchases.Module
	Payments    *payments.Module
	Treasury    *treasury.Module
	Accounting  *accounting.Module
	Assets      *assets.Module
	Documents   *documents.Module
	Shipments   *shipments.Module
	Work        *work.Module
	Financial   *financial.Module
	Exchange    *exchange.Module
	Modules     *modules.Module
	Imports     *imports.Module
	Exports     *exports.Module
	Audit       *audit.Module

	jwt      *jwtauth.HS256
	resolver *authorization.Resolver
	relays   []*outbox.Relay

	// Deliveries keeps, for each listener, the messages it could not take.
	Deliveries *mailbox.Office
	system     context.Context
}

// systemSubject is who the host is when it runs what nobody asked for by hand.
var systemSubject = fw.MustParseUUID("00000000-0000-7000-8000-00000000c0de")

// Migrations lists the schema of every context that stores something, in the order they are
// composed. The deployment migrates with it and the host verifies it before it starts.
func Migrations() []sqlrepo.MigrationSet {
	return []sqlrepo.MigrationSet{geoinfra.Migrations(), facinfra.Migrations(), parinfra.Migrations(), secinfra.Migrations(), proinfra.Migrations(),
		fisinfra.Migrations(), hrinfra.Migrations(), prlinfra.Migrations(), recinfra.Migrations(), invinfra.Migrations(), ordinfra.Migrations(),
		bilinfra.Migrations(), purinfra.Migrations(), payinfra.Migrations(), treinfra.Migrations(), accinfra.Migrations(), astinfra.Migrations(),
		docinfra.Migrations(), shpinfra.Migrations(), wrkinfra.Migrations(), fininfra.Migrations(), exginfra.Migrations(), modinfra.Migrations(),
		impinfra.Migrations(), expinfra.Migrations(), mailbox.Migrations()}
}

// Permissions lists what every business context checks: what Security is told at start-up.
func Permissions() []authz.Permission {
	return slices.Concat(geoapp.Permissions(), facapp.Permissions(), parapp.Permissions(), proapp.Permissions(), fisapp.Permissions(), hrapp.Permissions(),
		prlapp.Permissions(), recapp.Permissions(), invapp.Permissions(), ordapp.Permissions(), bilapp.Permissions(), purapp.Permissions(),
		payapp.Permissions(), treapp.Permissions(), accapp.Permissions(), astapp.Permissions(), docapp.Permissions(), shpapp.Permissions(),
		wrkapp.Permissions(), finapp.Permissions(), exgapp.Permissions(), modapp.Permissions(), impapp.Permissions(), expapp.Permissions(),
		audapp.Permissions())
}

// Compose builds every context on sw, each with the ports of the others it asks for.
func Compose(sw *hotswap.Switch, o Options) (*Host, error) {
	jwt, err := jwtauth.New(jwtauth.Config{Secret: o.JWTSecret})
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}
	if o.Files == nil {
		o.Files = expinfra.NewMemoryFiles()
	}
	h := &Host{Switch: sw, Broker: inprocess.NewBroker(), jwt: jwt}

	h.Geography = geography.Compose(sw)
	h.Facilities = facilities.Compose(sw, facilities.WithAddressChecker(facinfra.GeographyAddresses{Checker: h.Geography.Ports}))
	h.Parties = parties.Compose(sw, nil, parties.WithAddressChecker(parinfra.GeographyAddresses{Checker: h.Geography.Ports}),
		parties.WithFacilityDirectory(parinfra.FacilitiesDirectory{Directory: h.Facilities.Directory}))
	h.Security = security.Compose(sw, security.WithTokenIssuer(secdist.HS256Issuer{JWT: jwt}),
		security.WithParties(secinfra.PartiesDirectory{Directory: h.Parties.Directory, Organizations: h.Parties.Organizations}))
	h.Products = products.Compose(sw)
	h.Fiscal = fiscal.Compose(sw, fisinfra.PartiesIdentities{TaxIdentities: h.Parties.TaxIdentities})
	h.HR = hr.Compose(sw, hr.WithOrganizations(hrinfra.PartiesOrganizations{Hierarchy: h.Parties.Organizations, Membership: h.Parties.Organizations}),
		hr.WithFacilities(hrinfra.FacilitiesDirectory{Directory: h.Facilities.Directory}))
	h.Payroll = payroll.Compose(sw, prlinfra.HRStaff{Staff: h.HR.Staff})
	h.Receivables = receivables.Compose(sw, &SellerCalendar{Parties: h.Parties, Geography: h.Geography, system: func() context.Context { return h.system }})
	h.Inventory = inventory.Compose(sw, invinfra.ProductsCatalog{Catalog: h.Products.Catalog})
	h.Orders = orders.Compose(sw, ordinfra.ProductsCatalog{Catalog: h.Products.Catalog, Pricing: h.Products.Pricing},
		ordinfra.ReceivablesCredit{Exposure: h.Receivables.Credit})
	h.Billing = billing.Compose(sw, bilinfra.FiscalTaxes{Engine: h.Fiscal.TaxEngine}, bilinfra.PartiesIdentities{TaxIdentities: h.Parties.TaxIdentities})
	h.Purchases = purchases.Compose(sw, purinfra.FiscalTaxes{Engine: h.Fiscal.TaxEngine})
	h.Payments = payments.Compose(sw, payinfra.PayrollNetPay{Remittance: h.Payroll.Remittance})
	h.Treasury = treasury.Compose(sw, treinfra.ReceivablesDueItems{Collectable: h.Receivables.Collectable},
		treinfra.PaymentsPayables{Payable: h.Payments.Payable}, treinfra.PartiesIdentities{TaxIdentities: h.Parties.TaxIdentities})
	h.Accounting = accounting.Compose(sw)
	h.Assets = assets.Compose(sw)
	h.Documents = documents.Compose(sw)
	h.Shipments = shipments.Compose(sw)
	h.Work = work.Compose(sw)
	institutions := FinancialInstitutions{Parties: h.Parties, system: func() context.Context { return h.system }}
	h.Financial = financial.Compose(sw, institutions)
	h.Exchange = exchange.Compose(sw, PromotionCodes{Collaborators: h.Parties.Collaborators})
	h.Modules = modules.Compose(sw, modules.WithDerivation(SectorCapabilities{Institutions: institutions}))
	h.Imports = imports.Compose(sw).Offer(impdomain.Sage{}, impdomain.Apiscore{Entity: o.ApiscoreEntity}, impdomain.Holidays{}).Load(
		LegalEntities{Parties: h.Parties}, Departments{Parties: h.Parties, UoW: sw},
		WorkCenters{Facilities: h.Facilities}, People{Parties: h.Parties, UoW: sw}, Employments{HR: h.HR, Parties: h.Parties, UoW: sw},
		Positions{HR: h.HR, UoW: sw}, WorkPlaces{Parties: h.Parties}, Customers(h.Parties, sw), Suppliers(h.Parties, sw),
		TaxRates{Fiscal: h.Fiscal}, ChartAccounts{Accounting: h.Accounting}, OwnAccounts{Treasury: h.Treasury},
		HeldAccounts{Financial: h.Financial, UoW: sw}, HolidayCalendar{Geography: h.Geography})
	h.Exports = exports.Compose(sw, o.Files).Offer(CustomerAccounts{Financial: h.Financial}).Offer(PartyLists(h.Parties, h.HR)...).
		Offer(TradeLists(h.Parties, h.Billing, h.Orders, h.Receivables, h.Purchases)...).
		Offer(BookLists(h.Parties, h.Accounting, h.Payments, h.Assets)...)
	h.Audit = audit.Compose()
	h.histories()

	// The Published Language: what each context publishes reaches those that listen to it. Each
	// listener has a mailbox: what it cannot take is kept for it and the others are not held back.
	h.Deliveries = mailbox.New(sw)
	for _, c := range []mailbox.Listener{h.Accounting.Parking, h.Billing.Consumer, h.Documents.Consumer, h.Fiscal.Consumer, h.Inventory.Consumer, h.Orders.Consumer, h.Parties.Consumer,
		h.Payments.Consumer, h.Receivables.Consumer, h.Shipments.Consumer} {
		h.Broker.Subscribe(c.Name(), h.Deliveries.For(c))
	}
	for _, r := range []interface {
		Relay(application.MessageSender, ...outbox.RelayOption) *outbox.Relay
	}{h.Parties, h.Facilities, h.Security, h.Products, h.Fiscal, h.HR, h.Payroll, h.Receivables, h.Inventory, h.Orders, h.Billing, h.Purchases, h.Payments,
		h.Treasury, h.Assets, h.Shipments, h.Work, h.Financial, h.Exchange, h.Modules, h.Imports} {
		h.relays = append(h.relays, r.Relay(h.Broker))
	}

	h.resolver = authorization.NewResolver(h.Security.Directory, authorization.Options{ServicePrincipals: o.ServicePrincipals})
	ac, err := authz.NewContext(authz.Context{Subject: systemSubject, SubjectName: "karpo-host", Kind: authz.Service,
		Permissions: []authz.Permission{authz.Wildcard}})
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}
	ac.GlobalAdmin = true
	h.system = authz.WithContext(context.Background(), ac)
	return h, nil
}

// Started is what starting found and did.
type Started struct {
	Permissions int    // declared to Security
	Features    int    // added to the catalog of modules
	Bootstrap   string // what happened with the first administrator: created, not needed, or no credentials in the environment
}

// Start prepares a composed host on a migrated backend: Security learns the permissions of every
// context, the catalog of modules is completed and, if nobody administers the installation yet,
// the administrator the environment names is created. It is safe to call on every start.
func (h *Host) Start(ctx context.Context) (Started, error) {
	var out Started
	declared := Permissions()
	if _, err := h.Security.SyncCatalog(ctx, declared...); err != nil {
		return out, fmt.Errorf("host: permissions: %w", err)
	}
	out.Permissions = len(declared)
	n, err := h.Modules.EnsureCatalog(ctx)
	if err != nil {
		return out, fmt.Errorf("host: modules: %w", err)
	}
	out.Features = n
	if out.Bootstrap, err = h.Security.BootstrapFromEnv(ctx); err != nil {
		return out, fmt.Errorf("host: bootstrap: %w", err)
	}
	return out, nil
}

// Handler serves the routes of every context: the session routes are open, everything else needs
// a session and goes through the permissions and the scope of who calls.
func (h *Host) Handler() http.Handler {
	protected := http.NewServeMux()
	for _, m := range []distribution.EndpointModule{h.Geography.HTTP, h.Geography.HolidaysHTTP, h.Facilities, h.Parties.HTTP, h.Security.HTTP, h.Products, h.Fiscal, h.HR, h.Payroll,
		h.Receivables, h.Inventory, h.Orders, h.Billing, h.Purchases, h.Payments, h.Treasury, h.Accounting, h.Assets, h.Documents, h.Shipments, h.Work,
		h.Financial, h.Exchange, h.Modules, h.Imports, h.Exports, h.Audit, h.Deliveries} {
		m.RegisterRoutes(protected)
	}
	mux := http.NewServeMux()
	h.Security.HTTP.RegisterPublicRoutes(mux)
	mux.Handle("/", distribution.Chain(protected, distribution.Authorize(h.jwt, h.resolver)))
	return distribution.Chain(mux, distribution.Correlation())
}

// Deliver carries what the contexts have published to those that listen, until nothing is left.
// It returns how many messages moved. A message a listener cannot take stays for the next time;
// the others go on.
func (h *Host) Deliver(ctx context.Context) (int, error) {
	total := 0
	var failed error
	for moved := true; moved; {
		moved = false
		for _, r := range h.relays {
			n, err := r.RelayOnce(ctx)
			total += n
			moved = moved || n > 0
			failed = errors.Join(failed, err)
		}
		if failed != nil {
			break
		}
	}
	return total, failed
}

// Chores is what a round of the scheduler did.
type Chores struct {
	ImportsClosed  int // runs whose process died
	ExportsWritten int
	ExportsExpired int
	ExportsGivenUp int
	Reservations   int // of currency, nobody collected in time
	Quotes         int // nobody answered within their validity
	Postings       int // facts Accounting had kept and could post now
	Redelivered    int // messages a listener could not take before and took now
}

// MaxExportsPerRound bounds how many exports a round writes, so the rest of the chores get their
// turn.
const MaxExportsPerRound = 20

// RunChores does once what the contexts leave for a scheduler: it closes the imports whose process
// died, writes the exports that wait and removes the files kept long enough, and closes, company
// by company, the currency reservations and the quotes whose time went by, and posts what
// Accounting had kept for when it could. One chore that fails does not stop the others; every
// failure comes back.
func (h *Host) RunChores(ctx context.Context) (Chores, error) {
	var out Chores
	var failed error
	note := func(what string, err error) {
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", what, err))
		}
	}
	// As the transport delivers: without anybody's session.
	again, err := h.Deliveries.Redeliver(ctx, false)
	note("deliveries", err)
	out.Redelivered = again.Delivered
	ctx = authz.WithContext(ctx, authzOf(h.system))

	closed, err := h.Imports.Service.CloseStale.Handle(ctx, impapp.CloseStale{})
	note("imports", err)
	out.ImportsClosed = len(closed.Runs)

	for range MaxExportsPerRound {
		ran, err := h.Exports.Service.RunNext.Handle(ctx, expapp.RunNext{})
		if ran.Job != nil {
			out.ExportsWritten++
		}
		note("exports", err)
		if ran.Job == nil {
			break
		}
	}
	purged, err := h.Exports.Service.Purge.Handle(ctx, expapp.Purge{})
	note("exports purge", err)
	out.ExportsExpired, out.ExportsGivenUp = len(purged.Expired), len(purged.GivenUp)

	companies, err := h.Parties.Organizations.All(ctx)
	note("companies", err)
	for _, c := range companies {
		expired, err := h.Exchange.Service.ExpireDue.Handle(ctx, exgapp.ExpireDue{Company: c.ID})
		note("reservations of "+c.Name, err)
		out.Reservations += expired.Expired
		quotes, err := h.Orders.Service.ExpireQuotes.Handle(ctx, ordapp.ExpireQuotes{Company: c.ID})
		note("quotes of "+c.Name, err)
		out.Quotes += len(quotes.Numbers)
	}
	retried, err := h.Accounting.Service.RetryParked.Handle(ctx, accapp.RetryParked{})
	note("accounting", err)
	out.Postings = retried.Posted
	return out, failed
}

func authzOf(ctx context.Context) *authz.Context {
	ac, _ := authz.FromContext(ctx)
	return ac
}

// Run delivers and does the chores until ctx ends: messages every deliver, chores every chores.
// What fails is reported and tried again on the next round.
func (h *Host) Run(ctx context.Context, deliver, chores time.Duration, report func(error)) {
	if report == nil {
		report = func(error) {}
	}
	d, c := time.NewTicker(deliver), time.NewTicker(chores)
	defer d.Stop()
	defer c.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.C:
			if _, err := h.Deliver(ctx); err != nil && ctx.Err() == nil {
				report(err)
			}
		case <-c.C:
			if _, err := h.RunChores(ctx); err != nil && ctx.Err() == nil {
				report(err)
			}
		}
	}
}
