package application

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos del contexto (los recursos Parties/GeographicBoundary del C# y los datos de referencia).
var (
	PermBoundaryRead  = authz.MustPermission("Geography.Boundary.Read")
	PermReferenceRead = authz.MustPermission("Geography.Reference.Read")
)

// SearchBoundaries busca límites por nombre, tipo, padre (hijos directos) o agrupación.
type SearchBoundaries struct {
	Text       string
	Type       string
	Parent     string
	Grouping   string
	Country    string // ISO 3166-1 alfa-2
	Level      string // country, 1, 2, 3 o local
	Page, Size int
}

// GetBoundary carga un límite con sus antepasados y sus agrupaciones.
type GetBoundary struct{ ID string }

// LookupPostalCode lista los límites de un código postal en un país.
type LookupPostalCode struct{ Country, Code string }

// CheckPostalAddress valida la parte geográfica de una dirección.
type CheckPostalAddress = contracts.PostalAddressQuery

// ListCountries lista los perfiles de país.
type ListCountries struct{}

// SearchCountries busca países. IDs son códigos alfa-2 (sin distinguir mayúsculas; uno bien
// formado que no existe sólo no está). Text busca en el nombre o es un alfa-2 o alfa-3 exacto. EU,
// Eurozone y SEPA valen "", "true" o "false". Sort es alpha2 (por omisión), alpha3 o name, con
// alpha2 para deshacer empates.
type SearchCountries struct {
	IDs                []string
	ByIDs              bool
	Text               string
	EU, Eurozone, SEPA string
	Sort               string
	Desc               bool
	Page, Size         int
}

// GetCountry carga un país por su código ISO.
type GetCountry struct{ Alpha2 string }

// ListCatalog lista un catálogo de referencia: «boundary-types», «currencies», «languages»,
// «time-zones» o «street-types» (opcionalmente de un país).
type ListCatalog struct{ Name, Country string }

// BoundaryDTO es la representación de transporte de un límite.
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
	Country      string `json:"country,omitempty"`
	Level        string `json:"level,omitempty"`
	Parent       string `json:"parent,omitempty"`
	Active       bool   `json:"active"`
	// Code es la clave pública del límite en el mapa (ISO 3166 o INE; la isla, su NUTS) y
	// ParentCode la de su primer antecesor en el mapa. Vacíos si el límite no está en el mapa.
	Code       string `json:"code,omitempty"`
	ParentCode string `json:"parentCode,omitempty"`
}

// BoundaryLevelDTO da nombre a un nivel de la escalera en un país. Classification dice qué
// clasificación oficial es el nivel en ese país («NUTS 2», «LAU»), cuando la tiene.
type BoundaryLevelDTO struct {
	Country        string `json:"country"`
	Level          string `json:"level"`
	Name           string `json:"name"`
	Administrative bool   `json:"administrative"`
	Principal      bool   `json:"principal"`
	Classification string `json:"classification,omitempty"`
}

func classification(l domain.CountryLevel) string {
	switch l.Level {
	case domain.Level1, domain.Level2, domain.Level3:
		return "NUTS " + string(l.Level) + " (" + domain.NUTSVersion + ")"
	case domain.LevelLocal:
		return "LAU"
	}
	return ""
}

// BoundaryDetailDTO añade los antepasados (el más cercano primero) y las agrupaciones.
type BoundaryDetailDTO struct {
	BoundaryDTO
	Ancestors []contracts.BoundaryRef `json:"ancestors"`
	Groupings []contracts.BoundaryRef `json:"groupings"`
}

// PostalCodeDTO es una entrada de código postal.
type PostalCodeDTO struct {
	ID       string                `json:"id"`
	Code     string                `json:"code"`
	Boundary contracts.BoundaryRef `json:"boundary"`
}

// CountryDTO son los datos de referencia completos de un país.
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

// Service expone las consultas del contexto. Los datos de referencia se mantienen con migraciones.
type Service struct {
	SearchBoundaries   app.QueryHandler[SearchBoundaries, fw.Page[BoundaryDTO]]
	GetBoundary        app.QueryHandler[GetBoundary, BoundaryDetailDTO]
	LookupPostalCode   app.QueryHandler[LookupPostalCode, []PostalCodeDTO]
	CheckPostalAddress app.QueryHandler[CheckPostalAddress, contracts.PostalAddressResult]
	ListCountries      app.QueryHandler[ListCountries, []contracts.CountryInfo]
	SearchCountries    app.QueryHandler[SearchCountries, fw.Page[contracts.CountryInfo]]
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

func boundaryDTO(b *domain.Boundary, names map[domain.BoundaryTypeID]string, parents map[domain.BoundaryID]string) BoundaryDTO {
	c := b.Codes()
	d := BoundaryDTO{ID: b.ID().String(), Name: b.Name(), Type: b.Type().String(), TypeName: names[b.Type()], GeoCode: c.Geo,
		Abbreviation: c.Abbreviation, INE: c.INE, NUTS: c.NUTS, LAU: c.LAU, Country: b.Position().Country,
		Level: string(b.Position().Level), Active: b.IsActive()}
	if p, ok := b.Parent(); ok {
		d.Parent = p.String()
	}
	if code, _, ok := b.MapCode(); ok {
		d.Code, d.ParentCode = code, parents[b.ID()]
	}
	return d
}

func invalid(field, msg string) error {
	var v fw.Validation
	v.Add(field, "format", msg)
	return v.Err()
}

// NewService monta las consultas sobre los puertos.
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
			if c := strings.TrimSpace(q.Country); c != "" {
				country, err := vocab.NewCountryCode(c)
				if err != nil {
					return fw.Page[BoundaryDTO]{}, invalid("country", "country must be an ISO 3166-1 alpha-2 code")
				}
				parts = append(parts, domain.FieldCountry.Eq(country.String()))
			}
			if strings.TrimSpace(q.Level) != "" {
				level, err := domain.ParseLevel(q.Level)
				if err != nil {
					return fw.Page[BoundaryDTO]{}, err
				}
				parts = append(parts, domain.FieldLevel.Eq(string(level)))
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
			parents, err := mapParents(ctx, p, page.Items)
			if err != nil {
				return fw.Page[BoundaryDTO]{}, err
			}
			return fw.MapPage(page, func(b *domain.Boundary) BoundaryDTO { return boundaryDTO(b, names, parents) }), nil
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
			parents, err := mapParents(ctx, p, []*domain.Boundary{b})
			if err != nil {
				return BoundaryDetailDTO{}, err
			}
			return BoundaryDetailDTO{BoundaryDTO: boundaryDTO(b, names, parents), Ancestors: ancestors, Groupings: groupings}, nil
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
			return countryInfos(ctx, p, nil)
		}),

		SearchCountries: guard(PermReferenceRead, func(ctx context.Context, q SearchCountries) (fw.Page[contracts.CountryInfo], error) {
			return searchCountries(ctx, p, q)
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
			case "boundary-levels":
				all, err := p.Catalogs.CountryLevels(ctx)
				if err != nil {
					return nil, err
				}
				out := []BoundaryLevelDTO{}
				for _, l := range all {
					if q.Country == "" || strings.EqualFold(l.Country.String(), q.Country) {
						out = append(out, BoundaryLevelDTO{Country: l.Country.String(), Level: string(l.Level), Name: l.Name,
							Administrative: l.Administrative, Principal: l.Principal, Classification: classification(l)})
					}
				}
				return out, nil
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

// countryInfos lee los países que cumplen where, por alfa-2, con el nombre de su límite.
func countryInfos(ctx context.Context, p Ports, where spec.Specification[*domain.Country]) ([]contracts.CountryInfo, error) {
	cs, err := p.Countries.Find(ctx, where, domain.FieldAlpha2.Asc())
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
}

// searchCountries valida la búsqueda, filtra los ids y las banderas con una especificación sobre
// geo_countries, y el texto, el orden y la página aquí: el nombre está en el límite (geo_boundaries),
// no en el país, y los países son unos 250.
func searchCountries(ctx context.Context, p Ports, q SearchCountries) (fw.Page[contracts.CountryInfo], error) {
	var v fw.Validation
	v.Require(q.Sort == "" || q.Sort == "alpha2" || q.Sort == "alpha3" || q.Sort == "name", "sortBy", "enum", "alpha2, alpha3, name")
	parts := []spec.Specification[*domain.Country]{}
	for _, f := range []struct {
		name, value string
		field       spec.Field[*domain.Country, bool]
	}{{"eu", q.EU, domain.FieldEU}, {"eurozone", q.Eurozone, domain.FieldEurozone}, {"sepa", q.SEPA, domain.FieldSEPA}} {
		v.Require(f.value == "" || f.value == "true" || f.value == "false", f.name, "enum", "true or false")
		if f.value != "" {
			parts = append(parts, f.field.Eq(f.value == "true"))
		}
	}
	var ids []string
	if q.ByIDs {
		ids = app.ParseIDs(&v, q.IDs, canonAlpha2)
	}
	if err := v.Err(); err != nil {
		return fw.Page[contracts.CountryInfo]{}, err
	}
	var found []contracts.CountryInfo
	if !q.ByIDs || len(ids) > 0 {
		if q.ByIDs {
			parts = append(parts, domain.FieldAlpha2.In(ids...))
		}
		all, err := countryInfos(ctx, p, spec.And(parts...))
		if err != nil {
			return fw.Page[contracts.CountryInfo]{}, err
		}
		text := strings.ToLower(strings.TrimSpace(q.Text))
		for _, c := range all {
			if text == "" || strings.Contains(strings.ToLower(c.Name), text) || strings.EqualFold(c.Alpha2, text) || strings.EqualFold(c.Alpha3, text) {
				found = append(found, c)
			}
		}
	}
	key := func(c contracts.CountryInfo) string {
		switch q.Sort {
		case "alpha3":
			return c.Alpha3
		case "name":
			return strings.ToLower(c.Name)
		}
		return c.Alpha2
	}
	slices.SortStableFunc(found, func(a, b contracts.CountryInfo) int {
		c := strings.Compare(key(a), key(b))
		if q.Desc {
			c = -c
		}
		if c == 0 {
			c = strings.Compare(a.Alpha2, b.Alpha2) // el desempate, por la identidad
		}
		return c
	})
	size := q.Size
	if size <= 0 {
		size = fw.DefaultPageSize
	}
	page := max(q.Page, 1)
	total := len(found)
	from, to := min((page-1)*size, total), min(page*size, total)
	return fw.NewPage(found[from:to:to], int64(total), page, size), nil
}

// canonAlpha2 acepta exactamente dos letras ASCII y las devuelve en mayúsculas.
func canonAlpha2(s string) (string, error) {
	if len(s) != 2 {
		return "", errors.New("not an alpha-2 code")
	}
	b := []byte(strings.ToUpper(s))
	for _, c := range b {
		if c < 'A' || c > 'Z' {
			return "", errors.New("not an alpha-2 code")
		}
	}
	return string(b), nil
}

type stringer string

func (s stringer) String() string { return string(s) }

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), pipeline.RequirePermission[In, Out](p))
}
