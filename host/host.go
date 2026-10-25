// Package host es la raíz de composición de Karpo: construye cada contexto delimitado sobre un
// mismo backend, da a cada uno los puertos que pide de los otros, lleva el Published Language entre
// ellos, declara sus permisos a Security, sirve sus rutas tras la autenticación y ejecuta lo que
// dejan para un planificador. Aquí nada es negocio: es cableado, y el único sitio que conoce todos
// los contextos.
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

// Options es lo que decide el despliegue.
type Options struct {
	// JWTSecret firma las sesiones; es obligatorio.
	JWTSecret []byte
	// Files guarda los ficheros de las exportaciones (nil: en memoria, se pierden al reiniciar).
	Files expapp.Files
	// ServicePrincipals son los procesos que llaman a la API con su propio token.
	ServicePrincipals []authz.ServicePrincipal
	// OnDeliveryGivenUp recibe aviso cuando se abandona un mensaje para un oyente tras demasiados
	// intentos: desde entonces espera a una persona. Nil: no se avisa a nadie.
	OnDeliveryGivenUp func(context.Context, mailbox.DTO)
}

// Host es Karpo compuesto.
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
	Exports     *exports.Module
	Audit       *audit.Module

	jwt      *jwtauth.HS256
	resolver *authorization.Resolver
	relays   []*outbox.Relay

	// Deliveries guarda, para cada oyente, los mensajes que no pudo recoger.
	Deliveries *mailbox.Office
	system     context.Context
}

// systemSubject es quién es el anfitrión cuando ejecuta lo que nadie pidió a mano.
var systemSubject = fw.MustParseUUID("00000000-0000-7000-8000-00000000c0de")

// Migrations lista el esquema de cada contexto que guarda algo, en el orden en que se componen. El
// despliegue migra con ella y el anfitrión la verifica antes de arrancar.
func Migrations() []sqlrepo.MigrationSet {
	return []sqlrepo.MigrationSet{geoinfra.Migrations(), facinfra.Migrations(), parinfra.Migrations(), secinfra.Migrations(), proinfra.Migrations(),
		fisinfra.Migrations(), hrinfra.Migrations(), prlinfra.Migrations(), recinfra.Migrations(), invinfra.Migrations(), ordinfra.Migrations(),
		bilinfra.Migrations(), purinfra.Migrations(), payinfra.Migrations(), treinfra.Migrations(), accinfra.Migrations(), astinfra.Migrations(),
		docinfra.Migrations(), shpinfra.Migrations(), wrkinfra.Migrations(), fininfra.Migrations(), exginfra.Migrations(), modinfra.Migrations(),
		expinfra.Migrations(), mailbox.Migrations()}
}

// Permissions lista lo que comprueba cada contexto de negocio: lo que se le dice a Security al arrancar.
func Permissions() []authz.Permission {
	return slices.Concat(geoapp.Permissions(), facapp.Permissions(), parapp.Permissions(), proapp.Permissions(), fisapp.Permissions(), hrapp.Permissions(),
		prlapp.Permissions(), recapp.Permissions(), invapp.Permissions(), ordapp.Permissions(), bilapp.Permissions(), purapp.Permissions(),
		payapp.Permissions(), treapp.Permissions(), accapp.Permissions(), astapp.Permissions(), docapp.Permissions(), shpapp.Permissions(),
		wrkapp.Permissions(), finapp.Permissions(), exgapp.Permissions(), modapp.Permissions(), expapp.Permissions(),
		audapp.Permissions())
}

// Compose construye cada contexto sobre sw, cada uno con los puertos de los otros que pide.
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
	// Las parties fusionadas, que los listados de los demás contextos dejan fuera salvo que se pidan
	// (docs/DECISIONES-COMUNES.md, D-100). Parties las lee dentro del proceso, sin los permisos del
	// usuario (D-103), como las identidades fiscales.
	merged := h.Parties.MergedParties
	h.Fiscal = fiscal.Compose(sw, fisinfra.PartiesIdentities{TaxIdentities: h.Parties.TaxIdentities})
	h.HR = hr.Compose(sw, hr.WithOrganizations(hrinfra.PartiesOrganizations{Hierarchy: h.Parties.Organizations, Membership: h.Parties.Organizations}),
		hr.WithFacilities(hrinfra.FacilitiesDirectory{Directory: h.Facilities.Directory}), hr.WithMergedParties(merged))
	h.Payroll = payroll.Compose(sw, prlinfra.HRStaff{Staff: h.HR.Staff}, payroll.WithMergedParties(merged))
	h.Receivables = receivables.Compose(sw, &SellerCalendar{Parties: h.Parties, Geography: h.Geography, system: func() context.Context { return h.system }},
		receivables.WithMergedParties(merged))
	h.Inventory = inventory.Compose(sw, invinfra.ProductsCatalog{Catalog: h.Products.Catalog})
	h.Orders = orders.Compose(sw, ordinfra.ProductsCatalog{Catalog: h.Products.Catalog, Pricing: h.Products.Pricing},
		ordinfra.ReceivablesCredit{Exposure: h.Receivables.Credit}, orders.WithBlocks(ordinfra.PartiesBlocks{TaxIdentities: h.Parties.TaxIdentities}),
		orders.WithMergedParties(merged))
	h.Billing = billing.Compose(sw, bilinfra.FiscalTaxes{Engine: h.Fiscal.TaxEngine}, bilinfra.PartiesIdentities{TaxIdentities: h.Parties.TaxIdentities},
		billing.WithMergedParties(merged))
	h.Purchases = purchases.Compose(sw, purinfra.FiscalTaxes{Engine: h.Fiscal.TaxEngine}, purchases.WithMergedParties(merged))
	h.Payments = payments.Compose(sw, payinfra.PayrollNetPay{Remittance: h.Payroll.Remittance}, payments.WithMergedParties(merged))
	h.Treasury = treasury.Compose(sw, treinfra.ReceivablesDueItems{Collectable: h.Receivables.Collectable},
		treinfra.PaymentsPayables{Payable: h.Payments.Payable}, treinfra.PartiesIdentities{TaxIdentities: h.Parties.TaxIdentities},
		treasury.WithMergedParties(merged))
	h.Accounting = accounting.Compose(sw)
	h.Assets = assets.Compose(sw)
	h.Documents = documents.Compose(sw, documents.WithMergedParties(merged))
	h.Shipments = shipments.Compose(sw, shipments.WithMergedParties(merged))
	h.Work = work.Compose(sw, work.WithMergedParties(merged))
	institutions := FinancialInstitutions{Parties: h.Parties, system: func() context.Context { return h.system }}
	h.Financial = financial.Compose(sw, institutions, financial.WithMergedParties(merged))
	h.Exchange = exchange.Compose(sw, PromotionCodes{Collaborators: h.Parties.Collaborators}, exchange.WithMergedParties(merged))
	h.Modules = modules.Compose(sw, modules.WithDerivation(SectorCapabilities{Institutions: institutions}))
	h.Exports = exports.Compose(sw, o.Files).Offer(CustomerAccounts{Financial: h.Financial}).Offer(PartyLists(h.Parties, h.HR)...).
		Offer(TradeLists(h.Parties, h.Billing, h.Orders, h.Receivables, h.Purchases)...).
		Offer(BookLists(h.Parties, h.Accounting, h.Payments, h.Assets)...)
	h.Audit = audit.Compose()
	h.histories()

	// El Published Language: lo que publica cada contexto llega a quienes lo escuchan. Cada oyente
	// tiene un buzón: lo que no puede recoger se le guarda y a los otros no se les retiene.
	h.Deliveries = mailbox.New(sw)
	h.Deliveries.OnGivenUp(o.OnDeliveryGivenUp)
	for _, c := range []mailbox.Listener{h.Accounting.Parking, h.Billing.Consumer, h.Documents.Consumer, h.Fiscal.Consumer, h.Inventory.Consumer, h.Orders.Consumer, h.Parties.Consumer,
		h.Payments.Consumer, h.Purchases.Consumer, h.Receivables.Consumer, h.Shipments.Consumer} {
		h.Broker.Subscribe(c.Name(), h.Deliveries.For(c))
	}
	for _, r := range []interface {
		Relay(application.MessageSender, ...outbox.RelayOption) *outbox.Relay
	}{h.Parties, h.Facilities, h.Security, h.Products, h.Fiscal, h.HR, h.Payroll, h.Receivables, h.Inventory, h.Orders, h.Billing, h.Purchases, h.Payments,
		h.Treasury, h.Assets, h.Shipments, h.Work, h.Financial, h.Exchange, h.Modules} {
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

// Started es lo que el arranque encontró e hizo.
type Started struct {
	Permissions int    // declarados a Security
	Features    int    // añadidas al catálogo de módulos
	Bootstrap   string // qué pasó con el primer administrador: creado, no hacía falta, o sin credenciales en el entorno
}

// Start prepara un anfitrión compuesto sobre un backend migrado: Security aprende los permisos de
// cada contexto, se completa el catálogo de módulos y, si nadie administra aún la instalación, se
// crea el administrador que nombra el entorno. Se puede llamar sin peligro en cada arranque.
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

// Handler sirve las rutas de cada contexto: las rutas de sesión están abiertas, todo lo demás
// necesita una sesión y pasa por los permisos y el ámbito de quien llama.
func (h *Host) Handler() http.Handler {
	protected := http.NewServeMux()
	for _, m := range []distribution.EndpointModule{h.Geography.HTTP, h.Geography.HolidaysHTTP, h.Facilities, h.Parties.HTTP, h.Security.HTTP, h.Products, h.Fiscal, h.HR, h.Payroll,
		h.Receivables, h.Inventory, h.Orders, h.Billing, h.Purchases, h.Payments, h.Treasury, h.Accounting, h.Assets, h.Documents, h.Shipments, h.Work,
		h.Financial, h.Exchange, h.Modules, h.Exports, h.Audit, h.Deliveries, h.Pairs()} {
		m.RegisterRoutes(protected)
	}
	mux := http.NewServeMux()
	h.Security.HTTP.RegisterPublicRoutes(mux)
	mux.Handle("/", distribution.Chain(protected, distribution.Authorize(h.jwt, h.resolver)))
	return distribution.Chain(mux, distribution.Correlation())
}

// Deliver lleva lo que han publicado los contextos a quienes escuchan, hasta que no queda nada.
// Devuelve cuántos mensajes se movieron. Un mensaje que un oyente no puede recoger se queda para la
// próxima vez; los otros siguen.
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

// Chores es lo que hizo una ronda del planificador.
type Chores struct {
	ExportsWritten int
	ExportsExpired int
	ExportsGivenUp int
	Reservations   int // de divisa, que nadie recogió a tiempo
	Quotes         int // que nadie contestó dentro de su validez
	Postings       int // hechos que Accounting había guardado y ahora pudo contabilizar
	Redelivered    int // mensajes que un oyente no pudo recoger antes y recogió ahora
}

// MaxExportsPerRound limita cuántas exportaciones escribe una ronda, para que el resto de las
// tareas tenga su turno.
const MaxExportsPerRound = 20

// RunChores hace una vez lo que los contextos dejan para un planificador: cierra las importaciones
// cuyo proceso murió, escribe las exportaciones que esperan y quita los ficheros guardados el tiempo
// suficiente, y cierra, empresa a empresa, las reservas de divisa y las ofertas cuyo plazo pasó, y
// contabiliza lo que Accounting había guardado para cuando pudiera. Una tarea que falla no detiene
// a las otras; se devuelven todos los fallos.
func (h *Host) RunChores(ctx context.Context) (Chores, error) {
	var out Chores
	var failed error
	note := func(what string, err error) {
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", what, err))
		}
	}
	// Como entrega el transporte: sin la sesión de nadie.
	again, err := h.Deliveries.Redeliver(ctx, false)
	note("deliveries", err)
	out.Redelivered = again.Delivered
	ctx = authz.WithContext(ctx, authzOf(h.system))

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

// Run entrega y hace las tareas hasta que acaba ctx: mensajes cada deliver, tareas cada chores.
// Lo que falla se informa y se vuelve a intentar en la ronda siguiente.
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
