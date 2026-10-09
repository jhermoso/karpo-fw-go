package host

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	recdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// placeFor is how long the place of a company is remembered: a schedule asks about many days in
// a row, and a company does not move every minute.
const placeFor = 5 * time.Minute

// SellerCalendar tells Receivables the days a seller does not collect on: Saturdays, Sundays and
// the holidays Geography keeps for where the seller is. Where it is, Parties says: the
// municipality of its postal address in force, or its country when the address names no
// municipality. A seller without an address has weekends only. It asks Parties as the host: it
// answers a fact about a company, it shows nothing of it.
type SellerCalendar struct {
	Parties   *parties.Module
	Geography *geography.Module
	system    func() context.Context

	mu     sync.Mutex
	places map[recdomain.OrganizationID]place
}

type place struct {
	boundary string
	until    time.Time
}

var _ recdomain.Calendar = (*SellerCalendar)(nil)

// where returns the boundary a seller is in ("" when nobody knows).
func (c *SellerCalendar) where(ctx context.Context, seller recdomain.OrganizationID) (string, error) {
	now := time.Now()
	c.mu.Lock()
	known, ok := c.places[seller]
	c.mu.Unlock()
	if ok && now.Before(known.until) {
		return known.boundary, nil
	}
	p, err := c.Parties.Service.Get.Handle(authz.WithContext(ctx, authzOf(c.system())), parapp.GetParty{ID: pardomain.PartyID{UUID: seller.UUID}})
	if err != nil && !errors.Is(err, fw.ErrNotFound) {
		return "", err
	}
	boundary := ""
	for _, contact := range p.Contacts {
		if !contact.Active || contact.Address == nil {
			continue
		}
		if contact.Address.GeoBoundary != "" {
			boundary = contact.Address.GeoBoundary
			break
		}
		if boundary == "" && contact.Address.Country != "" {
			if country, err := c.Geography.Ports.Country(ctx, contact.Address.Country); err == nil {
				boundary = country.Boundary
			}
		}
	}
	c.mu.Lock()
	if c.places == nil {
		c.places = map[recdomain.OrganizationID]place{}
	}
	c.places[seller] = place{boundary: boundary, until: now.Add(placeFor)}
	c.mu.Unlock()
	return boundary, nil
}

// IsHoliday implements Receivables' Calendar.
func (c *SellerCalendar) IsHoliday(ctx context.Context, seller recdomain.OrganizationID, d vocab.Date) (bool, error) {
	if wd := d.BaseTime().Weekday(); wd == time.Saturday || wd == time.Sunday {
		return true, nil
	}
	boundary, err := c.where(ctx, seller)
	if err != nil || boundary == "" {
		return false, err
	}
	return c.Geography.Holidays.IsHoliday(ctx, boundary, d.String())
}
