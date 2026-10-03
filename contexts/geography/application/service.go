package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Permissions of the context (the C# resources Parties/GeographicBoundary and the reference data).
var (
	PermBoundaryRead  = authz.MustPermission("Geography.Boundary.Read")
	PermReferenceRead = authz.MustPermission("Geography.Reference.Read")
)

// SearchBoundaries searches boundaries by name, type, parent (direct children) or grouping.
type SearchBoundaries struct {
	Text       string
	Type       string
	Parent     string
	Grouping   string
	Page, Size int
}

// GetBoundary loads a boundary with its ancestors and groupings.
type GetBoundary struct{ ID string }

// LookupPostalCode lists the boundaries of a postal code in a country.
type LookupPostalCode struct{ Country, Code string }

// CheckPostalAddress validates the geographic part of an address.
type CheckPostalAddress = contracts.PostalAddressQuery

// ListCountries lists the country profiles.
type ListCountries struct{}

// GetCountry loads a country by ISO code.
type GetCountry struct{ Alpha2 string }

// ListCatalog lists a reference catalog: "boundary-types", "currencies", "languages",
// "time-zones" or "street-types" (optionally of a country).
type ListCatalog struct{ Name, Country string }

// BoundaryDTO is the transport representation of a boundary.
type BoundaryDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	TypeName     string `json:"typeName,omitempty"`
	GeoCode      string `json:"geoCode,omitempty"`
	Abbreviation string `json:"abbreviation,omitempty"`
	INE          string `json:"ineCode,omitempty"`
	NUTS         string `json:"nutsCode,omitempty"`
	LAU          string `json:"lauCode,omitempty"`
	Parent       string `json:"parent,omitempty"`
	Active       bool   `json:"active"`
}

// BoundaryDetailDTO adds the ancestors (nearest first) and groupings.
type BoundaryDetailDTO struct {
	BoundaryDTO
	Ancestors []contracts.BoundaryRef `json:"ancestors"`
	Groupings []contracts.BoundaryRef `json:"groupings"`
}

// PostalCodeDTO is a postal code entry.
type PostalCodeDTO struct {
	ID       string                `json:"id"`
	Code     string                `json:"code"`
	Boundary contracts.BoundaryRef `json:"boundary"`
}

// CountryDTO is the full reference data of a country.
type CountryDTO struct {
	contracts.CountryInfo
	Numeric             int    `json:"numeric"`
	TrunkPrefix         string `json:"trunkPrefix,omitempty"`
	EEA                 bool   `json:"eea"`
	IBANLength          int    `json:"ibanLength,omitempty"`
	IBANPattern         string `json:"ibanPattern,omitempty"`
	PostalCodePattern   string `json:"postalCodePattern,omitempty"`
	PostalCodeRequired  bool   `json:"postalCodeRequired"`
	RequiresSubdivision bool   `json:"requiresSubdivision"`
}

// Service exposes the queries of the context. Reference data is maintained by migrations.
type Service struct {
	SearchBoundaries   app.QueryHandler[SearchBoundaries, fw.Page[BoundaryDTO]]
	GetBoundary        app.QueryHandler[GetBoundary, BoundaryDetailDTO]
	LookupPostalCode   app.QueryHandler[LookupPostalCode, []PostalCodeDTO]
	CheckPostalAddress app.QueryHandler[CheckPostalAddress, contracts.PostalAddressResult]
	ListCountries      app.QueryHandler[ListCountries, []contracts.CountryInfo]
	GetCountry         app.QueryHandler[GetCountry, CountryDTO]
	ListCatalog        app.QueryHandler[ListCatalog, any]
}

func typeNames(ctx context.Context, c domain.Catalogs) (map[domain.BoundaryTypeID]string, error) {
	types, err := c.BoundaryTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := map[domain.BoundaryTypeID]string{}
	for _, t := range types {
		out[t.ID] = t.Name
	}
	return out, nil
}

func boundaryDTO(b *domain.Boundary, names map[domain.BoundaryTypeID]string) BoundaryDTO {
	c := b.Codes()
	d := BoundaryDTO{ID: b.ID().String(), Name: b.Name(), Type: b.Type().String(), TypeName: names[b.Type()], GeoCode: c.Geo,
		Abbreviation: c.Abbreviation, INE: c.INE, NUTS: c.NUTS, LAU: c.LAU, Active: b.IsActive()}
	if p, ok := b.Parent(); ok {
		d.Parent = p.String()
	}
	return d
}

func invalid(field, msg string) error {
	var v fw.Validation
	v.Add(field, "format", msg)
	return v.Err()
}

// NewService wires the queries over the ports.
func NewService(p Ports) *Service {
	return &Service{
		SearchBoundaries: guard(PermBoundaryRead, func(ctx context.Context, q SearchBoundaries) (fw.Page[BoundaryDTO], error) {
			names, err := typeNames(ctx, p.Catalogs)
			if err != nil {
				return fw.Page[BoundaryDTO]{}, err
			}
			parts := []spec.Specification[*domain.Boundary]{}
			if t := strings.TrimSpace(q.Text); t != "" {
				parts = append(parts, domain.FieldBoundaryName.ContainsFold(t))
			}
			if q.Type != "" {
				id, err := fw.ParseUUID(q.Type)
				if err != nil {
					return fw.Page[BoundaryDTO]{}, invalid("type", "type must be a boundary type id")
				}
				parts = append(parts, domain.FieldBoundaryType.Eq(domain.BoundaryTypeID{UUID: id}))
			}
			for field, s := range map[string]string{"parent": q.Parent, "grouping": q.Grouping} {
				if s == "" {
					continue
				}
				id, err := domain.ParseBoundaryID(s)
				if err != nil {
					return fw.Page[BoundaryDTO]{}, invalid(field, field+" must be a boundary id")
				}
				if field == "parent" {
					parts = append(parts, domain.ChildrenOf(id))
				} else {
					parts = append(parts, domain.MembersOf(id))
				}
			}
			page, err := p.Boundaries.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.FieldBoundaryName.Asc()))
			if err != nil {
				return fw.Page[BoundaryDTO]{}, err
			}
			return fw.MapPage(page, func(b *domain.Boundary) BoundaryDTO { return boundaryDTO(b, names) }), nil
		}),

		GetBoundary: guard(PermBoundaryRead, func(ctx context.Context, q GetBoundary) (BoundaryDetailDTO, error) {
			id, err := domain.ParseBoundaryID(q.ID)
			if err != nil {
				return BoundaryDetailDTO{}, invalid("id", "invalid boundary id")
			}
			names, err := typeNames(ctx, p.Catalogs)
			if err != nil {
				return BoundaryDetailDTO{}, err
			}
			b, err := p.Boundaries.Get(ctx, id)
			if err != nil {
				return BoundaryDetailDTO{}, err
			}
			ancestors, err := p.Ancestors(ctx, q.ID)
			if err != nil {
				return BoundaryDetailDTO{}, err
			}
			groupings := []contracts.BoundaryRef{}
			if gs := b.Groupings(); len(gs) > 0 {
				refs, err := p.boundaries(ctx, gs)
				if err != nil {
					return BoundaryDetailDTO{}, err
				}
				for _, g := range refs {
					groupings = append(groupings, ref(g))
				}
			}
			return BoundaryDetailDTO{BoundaryDTO: boundaryDTO(b, names), Ancestors: ancestors, Groupings: groupings}, nil
		}),

		LookupPostalCode: guard(PermBoundaryRead, func(ctx context.Context, q LookupPostalCode) ([]PostalCodeDTO, error) {
			c, err := p.country(ctx, q.Country)
			if err != nil {
				return nil, err
			}
			code, err := c.CheckPostalCode(q.Code)
			if err != nil {
				return nil, err
			}
			entries, err := p.PostalCodes.Find(ctx, domain.FieldPostalCode.Eq(code))
			if err != nil {
				return nil, err
			}
			var ids []domain.BoundaryID
			for _, e := range entries {
				ids = append(ids, e.Boundary())
			}
			bs, err := p.boundaries(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := []PostalCodeDTO{}
			for _, e := range entries {
				if i := slices.IndexFunc(bs, func(b *domain.Boundary) bool { return b.ID() == e.Boundary() }); i >= 0 {
					out = append(out, PostalCodeDTO{ID: e.ID().String(), Code: e.Code(), Boundary: ref(bs[i])})
				}
			}
			slices.SortFunc(out, func(a, b PostalCodeDTO) int { return strings.Compare(a.Boundary.Name, b.Boundary.Name) })
			return out, nil
		}),

		CheckPostalAddress: guard(PermBoundaryRead, p.CheckPostalAddress),

		ListCountries: guard(PermReferenceRead, func(ctx context.Context, _ ListCountries) ([]contracts.CountryInfo, error) {
			cs, err := p.Countries.Find(ctx, nil, domain.FieldAlpha2.Asc())
			if err != nil {
				return nil, err
			}
			ids := make([]domain.BoundaryID, len(cs))
			for i, c := range cs {
				ids[i] = c.Boundary()
			}
			bs, err := p.boundaries(ctx, ids)
			if err != nil {
				return nil, err
			}
			names := map[domain.BoundaryID]string{}
			for _, b := range bs {
				names[b.ID()] = b.Name()
			}
			out := make([]contracts.CountryInfo, len(cs))
			for i, c := range cs {
				pr := c.Profile()
				out[i] = contracts.CountryInfo{Alpha2: pr.Alpha2.String(), Alpha3: pr.Alpha3, Name: names[pr.Boundary],
					Boundary: pr.Boundary.String(), CallingCode: pr.CallingCode, DefaultLocale: pr.DefaultLocale, EU: pr.EU,
					Eurozone: pr.Eurozone, SEPA: pr.SEPA}
			}
			return out, nil
		}),

		GetCountry: guard(PermReferenceRead, func(ctx context.Context, q GetCountry) (CountryDTO, error) {
			info, err := p.Country(ctx, q.Alpha2)
			if err != nil {
				return CountryDTO{}, err
			}
			c, err := p.country(ctx, q.Alpha2)
			if err != nil {
				return CountryDTO{}, err
			}
			pr := c.Profile()
			return CountryDTO{CountryInfo: info, Numeric: pr.Numeric, TrunkPrefix: pr.TrunkPrefix, EEA: pr.EEA,
				IBANLength: pr.IBANLength, IBANPattern: pr.IBANPattern, PostalCodePattern: pr.PostalCodePattern,
				PostalCodeRequired: pr.PostalCodeRequired, RequiresSubdivision: pr.RequiresSubdivision}, nil
		}),

		ListCatalog: guard(PermReferenceRead, func(ctx context.Context, q ListCatalog) (any, error) {
			switch q.Name {
			case "boundary-types":
				return p.Catalogs.BoundaryTypes(ctx)
			case "currencies":
				return p.Catalogs.Currencies(ctx)
			case "languages":
				return p.Catalogs.Languages(ctx)
			case "time-zones":
				return p.Catalogs.TimeZones(ctx)
			case "street-types":
				all, err := p.Catalogs.StreetTypes(ctx)
				if err != nil || q.Country == "" {
					return all, err
				}
				return slices.DeleteFunc(all, func(s domain.StreetType) bool { return !strings.EqualFold(s.Country.String(), q.Country) }), nil
			}
			return nil, fw.NotFound("geography.catalog", stringer(q.Name))
		}),
	}
}

type stringer string

func (s stringer) String() string { return string(s) }

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), pipeline.RequirePermission[In, Out](p))
}
