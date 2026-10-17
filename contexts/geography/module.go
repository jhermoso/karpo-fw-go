// Package geography compone el contexto delimitado de Geografía y datos de referencia sobre un
// backend intercambiable en caliente: las consultas, el módulo HTTP y los puertos que usan otros
// contextos (nomenclátor, comprobador de direcciones, referencia de países).
package geography

import (
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	gdist "github.com/jhermoso/karpo-fw-go/contexts/geography/distribution"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
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
	return &Module{Service: svc, Ports: ports, HTTP: gdist.NewModule(svc), Holidays: holidays, HolidaysHTTP: gdist.NewHolidaysModule(holidays)}
}

var _ domain.Catalogs = catalogs{}
