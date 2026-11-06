package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
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

// SQLProfiles guarda los perfiles en SQL.
type SQLProfiles struct{ db *sqlrepo.DB }

// Get implementa domain.InternalOrganizationProfiles: lee la fila de la organización.
func (s SQLProfiles) Get(ctx context.Context, org domain.PartyID) (domain.InternalOrganizationProfile, error) {
	d := s.db.Dialect()
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s", d.Quote("time_zone"),
		d.Quote(TableInternalOrganizationProfiles), d.Quote("internal_organization_id"), d.Placeholder(1)), d.UUIDValue(org.UUID))
	if err != nil {
		return domain.InternalOrganizationProfile{}, err
	}
	defer func() { _ = rows.Close() }()
	p := domain.InternalOrganizationProfile{Organization: org}
	if rows.Next() {
		var tz sql.NullString
		if err := rows.Scan(&tz); err != nil {
			return domain.InternalOrganizationProfile{}, err
		}
		p.TimeZone = tz.String
	}
	return p, rows.Err()
}

// Save implementa domain.InternalOrganizationProfiles: actualiza la fila, o la crea si no hay. Si
// otra petición la crea a la vez, el INSERT choca con la clave primaria (la única restricción de la
// tabla) y Save devuelve un conflicto: el caso de uso se repite entero en otra transacción, que ya
// encuentra la fila. No se repite aquí el UPDATE porque PostgreSQL anula la transacción tras el
// choque. (MySQL cuenta 0 filas en un UPDATE al mismo valor; no llega aquí, porque el caso de uso no
// guarda un perfil que no cambia.)
func (s SQLProfiles) Save(ctx context.Context, p domain.InternalOrganizationProfile) error {
	set := sqlrepo.Values{"time_zone": nullable(p.TimeZone)}
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
	store *memory.Store
	mu    sync.Mutex
	byID  map[domain.PartyID]domain.InternalOrganizationProfile
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
