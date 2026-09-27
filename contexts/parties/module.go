// Package parties composes the Parties bounded context: its ports bound to a hot-swappable
// backend, the use cases, the HTTP module, the party directory and the relay of its Published
// Language. A host mounts Module.HTTP and runs Module.Relay.
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

// Module is the composed context.
type Module struct {
	Service           *papp.Service
	Directory         contracts.Directory
	Organizations     papp.Organizations // Membership, OrganizationHierarchy, InternalOrganizationCatalog
	HTTP              *pdist.Module
	Outbox            application.OutboxStore // domain events, inside Parties
	IntegrationOutbox application.OutboxStore // Published Language
	Audit             application.AuditLog
}

// Option configures the composition.
type Option func(*papp.Deps)

// WithAddressChecker validates postal addresses with the Geography context.
func WithAddressChecker(c papp.AddressChecker) Option { return func(d *papp.Deps) { d.Addresses = c } }

// Compose builds the context on sw. idem may be nil.
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
	return &Module{Service: svc, Directory: dir, Organizations: orgs, HTTP: pdist.NewModule(svc, dir),
		Outbox: domainOutbox, IntegrationOutbox: integrationOutbox, Audit: audit}
}

// Relay forwards the Published Language to a transport (at-least-once).
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}
