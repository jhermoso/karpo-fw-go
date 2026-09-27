// Package geography composes the Geography and reference data bounded context on a hot-swap
// backend: the queries, the HTTP module and the ports other contexts use (gazetteer, address
// checker, country reference).
package geography

import (
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	gdist "github.com/jhermoso/karpo-fw-go/contexts/geography/distribution"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service *gapp.Service
	Ports   gapp.Ports // contracts.Gazetteer, contracts.AddressChecker, contracts.Reference
	HTTP    *gdist.Module
}

// Compose builds the context on sw. An in-memory backend must be filled with
// infrastructure.LoadMemory; SQL backends get the data from the migrations.
func Compose(sw *hotswap.Switch) *Module {
	ports := gapp.Ports{
		Boundaries:  hotswap.Repository(sw, infrastructure.BoundaryRepositoryFactory),
		PostalCodes: hotswap.Repository(sw, infrastructure.PostalCodeRepositoryFactory),
		Countries:   hotswap.Repository(sw, infrastructure.CountryRepositoryFactory),
		Catalogs:    catalogs{hotswap.Bind(sw, infrastructure.CatalogsFor)},
	}
	svc := gapp.NewService(ports)
	return &Module{Service: svc, Ports: ports, HTTP: gdist.NewModule(svc)}
}

var _ domain.Catalogs = catalogs{}
