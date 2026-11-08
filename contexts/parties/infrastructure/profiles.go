package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TableInternalOrganizationProfiles es la tabla del perfil de las empresas internas, con el nombre
// de Karpo C# y de Trench (D-133). Su clave es la party de la organización interna, no el id de su
// rol como en C# (docs/PARTIES.md §25).
const TableInternalOrganizationProfiles = "internal_organization_profile"

// TableInternalOrganizationCodePatterns es la tabla hija del perfil con el patrón de cada tipo de
// código de la empresa (D-144): una fila por (internal_organization_id, code_kind).
const TableInternalOrganizationCodePatterns = "internal_organization_code_pattern"

// SQLProfiles guarda los perfiles en SQL.
type SQLProfiles struct{ db *sqlrepo.DB }

// Get implementa domain.InternalOrganizationProfiles: lee la fila de la organización.
func (s SQLProfiles) Get(ctx context.Context, org domain.PartyID) (domain.InternalOrganizationProfile, error) {
	d := s.db.Dialect()
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s, %s, %s, %s, %s FROM %s WHERE %s = %s", d.Quote("time_zone"), d.Quote("line_rounding"),
		d.Quote("price_decimals"), d.Quote("installments_timing"), d.Quote("default_price_list"), d.Quote("automatic_pricing"),
		d.Quote(TableInternalOrganizationProfiles), d.Quote("internal_organization_id"), d.Placeholder(1)), d.UUIDValue(org.UUID))
	if err != nil {
		return domain.InternalOrganizationProfile{}, err
	}
	defer func() { _ = rows.Close() }()
	p := domain.InternalOrganizationProfile{Organization: org}
	if rows.Next() {
		var tz, rounding, timing sql.NullString
		var decimals sql.NullInt64
		var list, automatic any
		if err := rows.Scan(&tz, &rounding, &decimals, &timing, &list, &automatic); err != nil {
			return domain.InternalOrganizationProfile{}, err
		}
		p.TimeZone, p.LineRounding, p.InstallmentsTiming = tz.String, rounding.String, timing.String
		if decimals.Valid {
			n := int(decimals.Int64)
			p.PriceDecimals = &n
		}
		if list != nil {
			u, err := d.ParseUUID(list)
			if err != nil {
				return domain.InternalOrganizationProfile{}, fmt.Errorf("%s.default_price_list: %w", TableInternalOrganizationProfiles, err)
			}
			if !u.IsZero() {
				p.DefaultPriceList = u.String()
			}
		}
		if p.AutomaticPricing, err = optionalBool(automatic); err != nil {
			return domain.InternalOrganizationProfile{}, fmt.Errorf("%s.automatic_pricing: %w", TableInternalOrganizationProfiles, err)
		}
	}
	return p, rows.Err()
}

// optionalBool lee un booleano anulable tal como lo entrega cada driver (BOOLEAN, BIT, NUMBER(1),
// INTEGER o su texto).
func optionalBool(v any) (*bool, error) {
	var b bool
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bool:
		b = x
	case int64:
		b = x != 0
	case float64:
		b = x != 0
	case []byte:
		return optionalBool(string(x))
	default:
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(x))) { // también los números de Oracle (godror.Number)
		case "1", "true", "t":
			b = true
		case "0", "false", "f":
			b = false
		default:
			return nil, fmt.Errorf("cannot convert %v (%T) to bool", x, x)
		}
	}
	return &b, nil
}

// CodePattern implementa domain.InternalOrganizationProfiles.
func (s SQLProfiles) CodePattern(ctx context.Context, org domain.PartyID, kind string) (string, error) {
	d := s.db.Dialect()
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s AND %s = %s", d.Quote("pattern"),
		d.Quote(TableInternalOrganizationCodePatterns), d.Quote("internal_organization_id"), d.Placeholder(1), d.Quote("code_kind"), d.Placeholder(2)),
		d.UUIDValue(org.UUID), kind)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var pattern sql.NullString
	if rows.Next() {
		if err := rows.Scan(&pattern); err != nil {
			return "", err
		}
	}
	return pattern.String, rows.Err()
}

// SaveCodePattern implementa domain.InternalOrganizationProfiles: un patrón vacío borra la fila;
// si no, se actualiza o se crea. Dos primeros guardados a la vez chocan con la clave primaria y
// dan un conflicto, y el caso de uso se repite entero, como en Save.
func (s SQLProfiles) SaveCodePattern(ctx context.Context, org domain.PartyID, kind, pattern string) error {
	key := sqlrepo.Values{"internal_organization_id": org, "code_kind": kind}
	if pattern == "" {
		_, err := s.db.Delete(ctx, TableInternalOrganizationCodePatterns, key)
		return err
	}
	n, err := s.db.Update(ctx, TableInternalOrganizationCodePatterns, sqlrepo.Values{"pattern": pattern}, key)
	if err != nil || n > 0 {
		return err
	}
	err = s.db.Insert(ctx, TableInternalOrganizationCodePatterns, sqlrepo.Values{"internal_organization_id": org, "code_kind": kind, "pattern": pattern})
	if err != nil && s.db.Dialect().IsUniqueViolation(err) {
		return fmt.Errorf("%w: the %s pattern of %s was created at the same time", fw.ErrConflict, kind, org)
	}
	return err
}

// Save implementa domain.InternalOrganizationProfiles: actualiza la fila, o la crea si no hay. Si
// otra petición la crea a la vez, el INSERT choca con la clave primaria (la única restricción de la
// tabla) y Save devuelve un conflicto: el caso de uso se repite entero en otra transacción, que ya
// encuentra la fila. No se repite aquí el UPDATE porque PostgreSQL anula la transacción tras el
// choque. (MySQL cuenta 0 filas en un UPDATE al mismo valor; no llega aquí, porque el caso de uso no
// guarda un perfil que no cambia.)
func (s SQLProfiles) Save(ctx context.Context, p domain.InternalOrganizationProfile) error {
	var decimals any
	if p.PriceDecimals != nil {
		decimals = int64(*p.PriceDecimals)
	}
	var list, automatic any
	if p.DefaultPriceList != "" {
		u, err := fw.ParseUUID(p.DefaultPriceList)
		if err != nil {
			return err
		}
		list = u
	}
	if p.AutomaticPricing != nil {
		automatic = *p.AutomaticPricing
	}
	set := sqlrepo.Values{"time_zone": nullable(p.TimeZone), "line_rounding": nullable(p.LineRounding), "price_decimals": decimals,
		"installments_timing": nullable(p.InstallmentsTiming), "default_price_list": list, "automatic_pricing": automatic}
	n, err := s.db.Update(ctx, TableInternalOrganizationProfiles, set, sqlrepo.Values{"internal_organization_id": p.Organization})
	if err != nil || n > 0 {
		return err
	}
	set["internal_organization_id"] = p.Organization
	err = s.db.Insert(ctx, TableInternalOrganizationProfiles, set)
	if err != nil && s.db.Dialect().IsUniqueViolation(err) {
		return fmt.Errorf("%w: the profile of %s was created at the same time", fw.ErrConflict, p.Organization)
	}
	return err
}

// memoryProfiles guarda los perfiles de un almacén en memoria; un cambio se deshace si la unidad
// de trabajo falla, como en los repositorios en memoria.
type memoryProfiles struct {
	store    *memory.Store
	mu       sync.Mutex
	byID     map[domain.PartyID]domain.InternalOrganizationProfile
	patterns map[patternKey]string
}

type patternKey struct {
	org  domain.PartyID
	kind string
}

var memoryProfileStores sync.Map // *memory.Store -> *memoryProfiles

func (m *memoryProfiles) Get(_ context.Context, org domain.PartyID) (domain.InternalOrganizationProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.byID[org]; ok {
		return p, nil
	}
	return domain.InternalOrganizationProfile{Organization: org}, nil
}

func (m *memoryProfiles) Save(ctx context.Context, p domain.InternalOrganizationProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = map[domain.PartyID]domain.InternalOrganizationProfile{}
	}
	before, existed := m.byID[p.Organization]
	m.byID[p.Organization] = p
	m.store.OnRollback(ctx, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if existed {
			m.byID[p.Organization] = before
		} else {
			delete(m.byID, p.Organization)
		}
	})
	return nil
}

func (m *memoryProfiles) CodePattern(_ context.Context, org domain.PartyID, kind string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.patterns[patternKey{org, kind}], nil
}

func (m *memoryProfiles) SaveCodePattern(ctx context.Context, org domain.PartyID, kind, pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.patterns == nil {
		m.patterns = map[patternKey]string{}
	}
	k := patternKey{org, kind}
	before, existed := m.patterns[k]
	if pattern == "" {
		delete(m.patterns, k)
	} else {
		m.patterns[k] = pattern
	}
	m.store.OnRollback(ctx, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if existed {
			m.patterns[k] = before
		} else {
			delete(m.patterns, k)
		}
	})
	return nil
}

// ProfilesFor devuelve los perfiles de un backend.
func ProfilesFor(b hotswap.Backend) (domain.InternalOrganizationProfiles, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return SQLProfiles{db: db}, nil
	case *memory.Store:
		m, _ := memoryProfileStores.LoadOrStore(db, &memoryProfiles{store: db})
		return m.(*memoryProfiles), nil
	}
	return nil, unsupported(b)
}

// SwappableProfiles devuelve perfiles que siguen al backend del Switch.
func SwappableProfiles(sw *hotswap.Switch) domain.InternalOrganizationProfiles {
	return swappableProfiles{b: hotswap.Bind(sw, ProfilesFor)}
}

type swappableProfiles struct {
	b *hotswap.Binding[domain.InternalOrganizationProfiles]
}

func (s swappableProfiles) Get(ctx context.Context, org domain.PartyID) (p domain.InternalOrganizationProfile, err error) {
	err = s.b.With(ctx, func(ctx context.Context, x domain.InternalOrganizationProfiles) error {
		p, err = x.Get(ctx, org)
		return err
	})
	return p, err
}

func (s swappableProfiles) Save(ctx context.Context, p domain.InternalOrganizationProfile) error {
	return s.b.With(ctx, func(ctx context.Context, x domain.InternalOrganizationProfiles) error { return x.Save(ctx, p) })
}

func (s swappableProfiles) CodePattern(ctx context.Context, org domain.PartyID, kind string) (p string, err error) {
	err = s.b.With(ctx, func(ctx context.Context, x domain.InternalOrganizationProfiles) error {
		p, err = x.CodePattern(ctx, org, kind)
		return err
	})
	return p, err
}

func (s swappableProfiles) SaveCodePattern(ctx context.Context, org domain.PartyID, kind, pattern string) error {
	return s.b.With(ctx, func(ctx context.Context, x domain.InternalOrganizationProfiles) error {
		return x.SaveCodePattern(ctx, org, kind, pattern)
	})
}
