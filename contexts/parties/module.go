// Package parties compone el contexto delimitado Parties: sus puertos ligados a un backend
// intercambiable en caliente, los casos de uso, el módulo HTTP, el directorio de parties y el
// relay de su Published Language. Un anfitrión monta Module.HTTP y ejecuta Module.Relay.
package parties

import (
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pdist "github.com/jhermoso/karpo-fw-go/contexts/parties/distribution"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module es el contexto compuesto.
type Module struct {
	Service           *papp.Service
	Directory         contracts.Directory
	Organizations     papp.Organizations // Membership, OrganizationHierarchy, InternalOrganizationCatalog
	TaxIdentities     contracts.TaxIdentities
	Trials            contracts.Trials        // si la prueba de un prospect está en vigor
	Collaborators     contracts.Collaborators // de quién es un código de promoción
	HTTP              *pdist.Module
	Outbox            application.OutboxStore // eventos de dominio, dentro de Parties
	IntegrationOutbox application.OutboxStore // Published Language
	Audit             application.AuditLog
	// Consumer recibe los eventos de integración a los que reacciona Parties
	// (hr.employee-terminated.v1): hay que suscribirlo al transporte.
	Consumer *messaging.Consumer
}

// Option configura la composición.
type Option func(*papp.Deps)

// WithAddressChecker valida las direcciones postales con el contexto Geography.
func WithAddressChecker(c papp.AddressChecker) Option { return func(d *papp.Deps) { d.Addresses = c } }

// WithFacilityDirectory resuelve las instalaciones con el contexto Facilities (roles en
// instalaciones).
func WithFacilityDirectory(f papp.FacilityDirectory) Option {
	return func(d *papp.Deps) { d.FacilityDirectory = f }
}

// Compose construye el contexto sobre sw. idem puede ser nil.
func Compose(sw *hotswap.Switch, idem application.IdempotencyStore, opts ...Option) *Module {
	parties := hotswap.Repository(sw, infrastructure.PartyRepositoryFactory)
	relationships := hotswap.Repository(sw, infrastructure.RelationshipRepositoryFactory)
	domainOutbox := hotswap.Outbox(sw, infrastructure.OutboxFactory)
	integrationOutbox := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	recorder := outbox.Recorders(
		outbox.NewRecorder(domainOutbox),
		papp.Publications(messaging.NewRecorder(contracts.Source, integrationOutbox)),
	)
	deps := papp.Deps{
		Parties: parties, Relationships: relationships, Catalogs: infrastructure.SwappableCatalogs(sw),
		UoW: sw, Recorder: recorder, Audit: audit, Idempotency: idem,
	}
	for _, o := range opts {
		o(&deps)
	}
	svc := papp.NewService(deps)
	dir := papp.Directory{Parties: parties}
	orgs := papp.Organizations{Parties: parties, Relationships: relationships, Catalogs: infrastructure.SwappableCatalogs(sw)}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	papp.Subscribe(consumer, svc, relationships)
	trials := papp.Trials{Relationships: relationships, Catalogs: infrastructure.SwappableCatalogs(sw)}
	return &Module{Service: svc, Directory: dir, Organizations: orgs, TaxIdentities: papp.TaxIdentities{Parties: parties}, Trials: trials,
		Collaborators: papp.Collaborators{Relationships: relationships, Catalogs: infrastructure.SwappableCatalogs(sw)}, HTTP: pdist.NewModule(svc, dir),
		Outbox: domainOutbox, IntegrationOutbox: integrationOutbox, Audit: audit, Consumer: consumer}
}

// Relay reenvía el Published Language a un transporte (al menos una vez).
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}
