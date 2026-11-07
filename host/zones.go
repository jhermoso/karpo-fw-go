package host

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	_ "time/tzdata" // las zonas IANA van en el binario: no dependen del sistema (Windows, contenedores mínimos)

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	pcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CompanyZones dice a Billing la zona horaria de una empresa (docs/DECISIONES-COMUNES.md, D-133):
// la que guarda en su perfil de Parties; si no guarda ninguna, la principal del país de su
// documento fiscal en Geography; un país que Geography no conoce, o sin zona, es UTC. Así una
// empresa de Canarias con Atlantic/Canary no toma la hora de Madrid.
//
// La zona del perfil se lee siempre (una fila por su clave): un cambio vale desde la factura
// siguiente, también si se hace dos minutos antes de medianoche. Sólo se recuerda la del país.
//
// A los demás contextos les dice qué día es para una empresa, hoy o en un instante
// (application.CompanyToday): el
// país es el de la identidad fiscal de la empresa en Parties, como el del vendedor en Billing. Ese
// país se recuerda 5 minutos por empresa, como la zona de cada país: preguntarlo carga la party
// entera. La zona guardada sigue sin recordarse.
type CompanyZones struct {
	Geography  *geography.Module
	Profiles   pcontracts.CompanyTimeZones // opcional: sin él, sólo cuenta el país
	Identities pcontracts.TaxIdentities    // para Today: sin él, sin zona guardada es UTC

	mu        sync.Mutex
	known     map[string]zone    // por país
	countries map[string]country // por empresa
}

// country es el país de la identidad fiscal de una empresa y hasta cuándo se recuerda.
type country struct {
	code  string
	until time.Time
}

// zone es la zona de un país y hasta cuándo se recuerda: como el lugar de una empresa en
// SellerCalendar, para que un cambio en Geography llegue sin reiniciar.
type zone struct {
	loc   *time.Location
	until time.Time
}

// Location implementa billing/application.Zones.
func (z *CompanyZones) Location(ctx context.Context, company, country string) (*time.Location, error) {
	loc, err := z.stored(ctx, company)
	if err != nil || loc != nil {
		return loc, err
	}
	return z.ofCountry(ctx, country)
}

var _ application.CompanyToday = (*CompanyZones)(nil)

// DateOf implementa application.CompanyToday: la fecha de at en la zona de la empresa, con el
// mismo orden que Location (hoy es la del reloj del dominio). El país sólo se pregunta a Parties
// cuando la empresa no guarda zona.
func (z *CompanyZones) DateOf(ctx context.Context, company fw.UUID, at time.Time) (vocab.Date, error) {
	id := company.String()
	loc, err := z.stored(ctx, id)
	if err != nil {
		return vocab.Date{}, err
	}
	if loc == nil {
		code, err := z.countryOf(ctx, id)
		if err != nil {
			return vocab.Date{}, err
		}
		if loc, err = z.ofCountry(ctx, code); err != nil {
			return vocab.Date{}, err
		}
	}
	return vocab.DateOf(at.In(loc)), nil
}

// countryOf es el país de la identidad fiscal de la empresa ("" si no tiene); se recuerda 5 minutos.
func (z *CompanyZones) countryOf(ctx context.Context, company string) (string, error) {
	if z.Identities == nil {
		return "", nil
	}
	now := time.Now()
	z.mu.Lock()
	known, ok := z.countries[company]
	z.mu.Unlock()
	if ok && now.Before(known.until) {
		return known.code, nil
	}
	ids, err := z.Identities.TaxIdentities(ctx, []string{company})
	if err != nil {
		return "", fmt.Errorf("zones: company %s: %w", company, err)
	}
	code := ids[company].Country
	z.mu.Lock()
	if z.countries == nil {
		z.countries = map[string]country{}
	}
	z.countries[company] = country{code: code, until: now.Add(placeFor)}
	z.mu.Unlock()
	return code, nil
}

// stored es la zona que la empresa guarda en su perfil; nil si no guarda ninguna. Parties sólo
// guarda códigos IANA que conoce, así que uno que no se cargue es como no tener ninguno.
func (z *CompanyZones) stored(ctx context.Context, company string) (*time.Location, error) {
	if z.Profiles == nil || company == "" {
		return nil, nil
	}
	code, err := z.Profiles.TimeZone(ctx, company)
	if err != nil {
		return nil, fmt.Errorf("zones: company %s: %w", company, err)
	}
	if code == "" {
		return nil, nil
	}
	loc, err := time.LoadLocation(code)
	if err != nil {
		return nil, nil
	}
	return loc, nil
}

// ofCountry es la zona principal del país en Geography; UTC si no hay. Se recuerda 5 minutos.
func (z *CompanyZones) ofCountry(ctx context.Context, country string) (*time.Location, error) {
	if country == "" {
		return time.UTC, nil
	}
	now := time.Now()
	z.mu.Lock()
	known, ok := z.known[country]
	z.mu.Unlock()
	if ok && now.Before(known.until) {
		return known.loc, nil
	}
	info, err := z.Geography.Ports.Country(ctx, country)
	if err != nil && !errors.Is(err, fw.ErrNotFound) && !errors.Is(err, fw.ErrValidation) { // país desconocido: UTC
		return nil, fmt.Errorf("zones: country %s: %w", country, err)
	}
	loc := time.UTC
	if err == nil && len(info.TimeZones) > 0 {
		if loc, err = time.LoadLocation(info.TimeZones[0]); err != nil {
			return nil, fmt.Errorf("zones: country %s: %w", country, err)
		}
	}
	z.mu.Lock()
	if z.known == nil {
		z.known = map[string]zone{}
	}
	z.known[country] = zone{loc: loc, until: now.Add(placeFor)}
	z.mu.Unlock()
	return loc, nil
}
