// Package application holds the use cases of the Geography and reference data context (queries,
// protected by permissions) and the implementation of its Open Host Service for other contexts.
package application

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaxDepth bounds every walk of the boundary hierarchy (continent to municipality is 6).
const MaxDepth = 10

// chunk keeps IN lists small (SQL Server accepts 2100 parameters per statement): a whole
// country has thousands of municipalities.
const chunk = 500

// Ports implements the contracts of the context (Gazetteer, AddressChecker, Reference). It serves
// other contexts, not users: the caller's own use case is what is authorized.
type Ports struct {
	Boundaries  domain.BoundaryRepository
	PostalCodes domain.PostalCodeRepository
	Countries   domain.CountryRepository
	Catalogs    domain.Catalogs
}

var (
	_ contracts.Gazetteer      = Ports{}
	_ contracts.AddressChecker = Ports{}
	_ contracts.Reference      = Ports{}
)

func parseBoundaryIDs(ids []string) []domain.BoundaryID {
	out := make([]domain.BoundaryID, 0, len(ids))
	for _, s := range ids {
		if id, err := domain.ParseBoundaryID(s); err == nil && !id.IsZero() && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// find runs a specification built from ids in chunks.
func find[T any](ctx context.Context, ids []domain.BoundaryID, run func(context.Context, []domain.BoundaryID) ([]T, error)) ([]T, error) {
	var out []T
	for start := 0; start < len(ids); start += chunk {
		part, err := run(ctx, ids[start:min(start+chunk, len(ids))])
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

func (p Ports) boundaries(ctx context.Context, ids []domain.BoundaryID) ([]*domain.Boundary, error) {
	return find(ctx, ids, func(ctx context.Context, part []domain.BoundaryID) ([]*domain.Boundary, error) {
		return p.Boundaries.Find(ctx, domain.BoundariesWithIDs(part...))
	})
}

func ref(b *domain.Boundary) contracts.BoundaryRef {
	return contracts.BoundaryRef{ID: b.ID().String(), Name: b.Name(), Type: b.Type().String(), Active: b.IsActive()}
}

// Resolve implements contracts.Gazetteer.
func (p Ports) Resolve(ctx context.Context, ids []string) (map[string]contracts.BoundaryRef, error) {
	if len(ids) > contracts.MaxBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxBatch)
	}
	bs, err := p.boundaries(ctx, parseBoundaryIDs(ids))
	if err != nil {
		return nil, err
	}
	out := make(map[string]contracts.BoundaryRef, len(bs))
	for _, b := range bs {
		out[b.ID().String()] = ref(b)
	}
	return out, nil
}

// Ancestors implements contracts.Gazetteer.
func (p Ports) Ancestors(ctx context.Context, id string) ([]contracts.BoundaryRef, error) {
	bid, err := domain.ParseBoundaryID(id)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid boundary id", fw.ErrValidation)
	}
	b, err := p.Boundaries.Get(ctx, bid)
	if err != nil {
		return nil, err
	}
	out := []contracts.BoundaryRef{}
	for range MaxDepth {
		parent, ok := b.Parent()
		if !ok {
			break
		}
		if b, err = p.Boundaries.Get(ctx, parent); err != nil {
			return nil, err
		}
		out = append(out, ref(b))
	}
	return out, nil
}

// Descendants implements contracts.Gazetteer: one query per level (in chunks).
func (p Ports) Descendants(ctx context.Context, ids []string) ([]string, error) {
	seen := parseBoundaryIDs(ids)
	level := slices.Clone(seen)
	known := map[domain.BoundaryID]bool{}
	for _, id := range seen {
		known[id] = true
	}
	for depth := 0; depth < MaxDepth && len(level) > 0; depth++ {
		children, err := find(ctx, level, func(ctx context.Context, part []domain.BoundaryID) ([]*domain.Boundary, error) {
			return p.Boundaries.Find(ctx, domain.ChildrenOf(part...))
		})
		if err != nil {
			return nil, err
		}
		level = nil
		for _, c := range children {
			if !known[c.ID()] {
				known[c.ID()] = true
				seen = append(seen, c.ID())
				level = append(level, c.ID())
			}
		}
	}
	out := make([]string, len(seen))
	for i, id := range seen {
		out[i] = id.String()
	}
	return out, nil
}

// CountryOf implements contracts.Gazetteer: it walks up, one level per query, until a country.
func (p Ports) CountryOf(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	current := map[domain.BoundaryID]domain.BoundaryID{}
	for _, id := range parseBoundaryIDs(ids) {
		current[id] = id
	}
	countryOf := map[domain.BoundaryID]domain.BoundaryID{}
	for depth := 0; depth <= MaxDepth && len(current) > 0; depth++ {
		var nodes []domain.BoundaryID
		for _, n := range current {
			if !slices.Contains(nodes, n) {
				nodes = append(nodes, n)
			}
		}
		bs, err := p.boundaries(ctx, nodes)
		if err != nil {
			return nil, err
		}
		byID := map[domain.BoundaryID]*domain.Boundary{}
		for _, b := range bs {
			byID[b.ID()] = b
		}
		for origin, node := range current {
			b, ok := byID[node]
			switch {
			case !ok:
				delete(current, origin)
			case b.Type() == domain.TypeCountry:
				countryOf[origin] = node
				delete(current, origin)
			default:
				if parent, ok := b.Parent(); ok {
					current[origin] = parent
				} else {
					delete(current, origin)
				}
			}
		}
	}
	var countries []domain.BoundaryID
	for _, c := range countryOf {
		if !slices.Contains(countries, c) {
			countries = append(countries, c)
		}
	}
	if len(countries) == 0 {
		return out, nil
	}
	cs, err := p.Countries.Find(ctx, domain.FieldCountryBoundary.In(countries...))
	if err != nil {
		return nil, err
	}
	alpha := map[domain.BoundaryID]string{}
	for _, c := range cs {
		alpha[c.Boundary()] = c.Alpha2()
	}
	for origin, c := range countryOf {
		if a, ok := alpha[c]; ok {
			out[origin.String()] = a
		}
	}
	return out, nil
}

func (p Ports) country(ctx context.Context, alpha2 string) (*domain.Country, error) {
	cs, err := p.Countries.Find(ctx, domain.CountryAlpha2(alpha2))
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		var v fw.Validation
		v.Add("country", "unknown", "unknown country "+alpha2)
		return nil, v.Err()
	}
	return cs[0], nil
}

// CheckPostalAddress implements contracts.AddressChecker: the code must have the country format
// and, when a boundary is given, belong to it. Without a boundary, a code with a single entry
// is resolved to it.
func (p Ports) CheckPostalAddress(ctx context.Context, q contracts.PostalAddressQuery) (contracts.PostalAddressResult, error) {
	c, err := p.country(ctx, q.Country)
	if err != nil {
		return contracts.PostalAddressResult{}, err
	}
	code, err := c.CheckPostalCode(q.PostalCode)
	if err != nil || code == "" {
		return contracts.PostalAddressResult{PostalCode: code}, err
	}
	where := domain.FieldPostalCode.Eq(code)
	if q.Boundary != "" {
		b, err := domain.ParseBoundaryID(q.Boundary)
		if err != nil {
			return contracts.PostalAddressResult{}, fmt.Errorf("%w: invalid boundary id", fw.ErrValidation)
		}
		where = where.And(domain.FieldPostalBoundary.Eq(b))
	}
	entries, err := p.PostalCodes.Find(ctx, where)
	if err != nil {
		return contracts.PostalAddressResult{}, err
	}
	res := contracts.PostalAddressResult{PostalCode: code, Boundary: q.Boundary}
	switch {
	case q.Boundary != "" && len(entries) == 0:
		var v fw.Validation
		v.Add("postalCode", "boundary", "the postal code "+code+" does not belong to the chosen municipality")
		return contracts.PostalAddressResult{}, v.Err()
	case len(entries) == 1:
		res.PostalCodeID, res.Boundary = entries[0].ID().String(), entries[0].Boundary().String()
	}
	return res, nil
}

// Country implements contracts.Reference.
func (p Ports) Country(ctx context.Context, alpha2 string) (contracts.CountryInfo, error) {
	c, err := p.country(ctx, alpha2)
	if err != nil {
		return contracts.CountryInfo{}, err
	}
	pr := c.Profile()
	info := contracts.CountryInfo{Alpha2: pr.Alpha2.String(), Alpha3: pr.Alpha3, Boundary: pr.Boundary.String(),
		CallingCode: pr.CallingCode, DefaultLocale: pr.DefaultLocale, EU: pr.EU, Eurozone: pr.Eurozone, SEPA: pr.SEPA,
		Currencies: []string{}, Languages: []string{}, TimeZones: []string{}}
	if b, err := p.Boundaries.Get(ctx, pr.Boundary); err == nil {
		info.Name = b.Name()
	}
	currencies, err := p.Catalogs.Currencies(ctx)
	if err != nil {
		return contracts.CountryInfo{}, err
	}
	languages, err := p.Catalogs.Languages(ctx)
	if err != nil {
		return contracts.CountryInfo{}, err
	}
	zones, err := p.Catalogs.TimeZones(ctx)
	if err != nil {
		return contracts.CountryInfo{}, err
	}
	slices.SortStableFunc(pr.Currencies, func(a, b domain.CountryCurrency) int { return boolFirst(a.Primary, b.Primary, a.Order-b.Order) })
	for _, cc := range pr.Currencies {
		if i := slices.IndexFunc(currencies, func(x domain.Currency) bool { return x.ID == cc.Currency }); i >= 0 {
			info.Currencies = append(info.Currencies, currencies[i].Code)
		}
	}
	slices.SortStableFunc(pr.Languages, func(a, b domain.CountryLanguage) int { return boolFirst(a.Default, b.Default, a.Order-b.Order) })
	for _, cl := range pr.Languages {
		if i := slices.IndexFunc(languages, func(x domain.Language) bool { return x.ID == cl.Language }); i >= 0 {
			info.Languages = append(info.Languages, languages[i].Code)
		}
	}
	slices.SortStableFunc(pr.TimeZones, func(a, b domain.CountryTimeZone) int { return boolFirst(a.Primary, b.Primary, 0) })
	for _, ct := range pr.TimeZones {
		if i := slices.IndexFunc(zones, func(x domain.TimeZone) bool { return x.ID == ct.TimeZone }); i >= 0 {
			info.TimeZones = append(info.TimeZones, zones[i].Code)
		}
	}
	return info, nil
}

func boolFirst(a, b bool, then int) int {
	switch {
	case a && !b:
		return -1
	case b && !a:
		return 1
	}
	return then
}

// CheckIBAN implements contracts.Reference.
func (p Ports) CheckIBAN(ctx context.Context, iban string) error {
	i, err := vocab.NewIBAN(iban)
	if err != nil {
		return err
	}
	c, err := p.country(ctx, i.Country())
	if err != nil {
		return err
	}
	return c.CheckIBAN(i)
}
