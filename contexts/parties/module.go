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
	HTTP              *pdist.Module
	Outbox            application.OutboxStore // domain events, inside Parties
	IntegrationOutbox application.OutboxStore // Published Language
	Audit             application.AuditLog
}

// Compose builds the context on sw. idem may be nil.
func Compose(sw *hotswap.Switch, idem application.IdempotencyStore) *Module {
	parties := hotswap.Repository(sw, infrastructure.PartyRepositoryFactory)
	relationships := hotswap.Repository(sw, infrastructure.RelationshipRepositoryFactory)
	domainOutbox := hotswap.Outbox(sw, infrastructure.OutboxFactory)
	integrationOutbox := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	recorder := outbox.Recorders(
		outbox.NewRecorder(domainOutbox),
		papp.Publications(messaging.NewRecorder(contracts.Source, integrationOutbox)),
	)
	svc := papp.NewService(papp.Deps{
		Parties: parties, Relationships: relationships, Catalogs: infrastructure.SwappableCatalogs(sw),
		UoW: sw, Recorder: recorder, Audit: audit, Idempotency: idem,
	})
	dir := papp.Directory{Parties: parties}
	return &Module{Service: svc, Directory: dir, HTTP: pdist.NewModule(svc, dir),
		Outbox: domainOutbox, IntegrationOutbox: integrationOutbox, Audit: audit}
}

// Relay forwards the Published Language to a transport (at-least-once).
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}
