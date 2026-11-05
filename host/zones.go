package host

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	_ "time/tzdata" // las zonas IANA van en el binario: no dependen del sistema (Windows, contenedores mínimos)

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// CountryZones dice a Billing la zona horaria de una empresa: la principal de su país en
// Geography. Una empresa de Canarias toma la de Madrid hasta que la zona sea un ajuste de la
// empresa interna. Un país que Geography no conoce, o sin zona, es UTC.
type CountryZones struct {
	Geography *geography.Module

	mu    sync.Mutex
	known map[string]zone
}

// zone es la zona de un país y hasta cuándo se recuerda: como el lugar de una empresa en
// SellerCalendar, para que un cambio en Geography llegue sin reiniciar.
type zone struct {
	loc   *time.Location
	until time.Time
}

// Location implementa billing/application.Zones.
func (z *CountryZones) Location(ctx context.Context, country string) (*time.Location, error) {
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
