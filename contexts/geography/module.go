// Package geography compone el contexto delimitado de Geografía y datos de referencia sobre un
// backend intercambiable en caliente: las consultas, el módulo HTTP y los puertos que usan otros
// contextos (nomenclátor, comprobador de direcciones, referencia de países).
package geography

import (
	"net/http"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	gdist "github.com/jhermoso/karpo-fw-go/contexts/geography/distribution"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module es el contexto compuesto.
type Module struct {
	Service *gapp.Service
	Ports   gapp.Ports // contracts.Gazetteer, contracts.AddressChecker, contracts.Reference
	HTTP    *gdist.Module
	// Holidays es el calendario de festivos (contracts.Calendar), y HolidaysHTTP sus rutas.
	Holidays     *gapp.Holidays
	HolidaysHTTP *gdist.HolidaysModule
	// UoW es la unidad de trabajo del contrato de mantenimiento (un PUT es una sola).
	UoW fw.UnitOfWork
}

// Compose construye el contexto sobre sw. Un backend en memoria hay que llenarlo con
// infrastructure.LoadMemory; los backends SQL toman los datos de las migraciones.
func Compose(sw *hotswap.Switch) *Module {
	ports := gapp.Ports{
		Boundaries:  hotswap.Repository(sw, infrastructure.BoundaryRepositoryFactory),
		PostalCodes: hotswap.Repository(sw, infrastructure.PostalCodeRepositoryFactory),
		Countries:   hotswap.Repository(sw, infrastructure.CountryRepositoryFactory),
		Catalogs:    catalogs{hotswap.Bind(sw, infrastructure.CatalogsFor)},
	}
	svc := gapp.NewService(ports)
	holidays := gapp.NewHolidays(hotswap.Repository(sw, infrastructure.HolidayRepositoryFactory), ports, sw)
	return &Module{Service: svc, Ports: ports, HTTP: gdist.NewModule(svc), Holidays: holidays, HolidaysHTTP: gdist.NewHolidaysModule(holidays),
		UoW: sw}
}

var (
	_ domain.Catalogs             = catalogs{}
	_ distribution.EndpointModule = (*Module)(nil)
)

// RegisterRoutes implementa distribution.EndpointModule: las rutas de consulta, las del calendario
// y el contrato de mantenimiento (/api/v1/countries y /api/v1/holidays).
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	m.HTTP.RegisterRoutes(mux)
	m.HolidaysHTTP.RegisterRoutes(mux)
	m.RegisterMaintenanceRoutes(mux)
}
