package host

import (
	"context"
	"strconv"

	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	parcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// The lists of Parties the grid exports (the C# ExportService served them from a switch of its
// own; here each is a dataset over the searches of the context that owns the list). They read as
// who asked for the file, so each file has what that person may see.

// PartyLists builds the lists of parties: all, and those of a kind or with a role.
func PartyLists(p *parties.Module, h *hr.Module) []expapp.Dataset {
	return []expapp.Dataset{
		partyList{p: p, key: "parties", title: "Participantes"},
		partyList{p: p, key: "persons", title: "Personas", kind: string(pardomain.KindPerson)},
		partyList{p: p, key: "organizations", title: "Organizaciones", kind: string(pardomain.KindOrganization)},
		partyList{p: p, key: "internal-organizations", title: "Organizaciones internas", role: pardomain.RoleInternalOrganization.String()},
		partyList{p: p, key: "customers", title: "Clientes", role: pardomain.RoleCustomer.String(), taxID: true},
		partyRoles{p: p},
		partyRelationships{p: p},
		employees{p: p, h: h},
	}
}

func page(cursor string) int {
	n, _ := strconv.Atoi(cursor)
	return max(n, 1)
}

func next(number, total int) string {
	if number >= total {
		return ""
	}
	return strconv.Itoa(number + 1)
}

func kindName(kind string) string {
	if kind == string(pardomain.KindPerson) {
		return "Persona"
	}
	return "Organización"
}

func status(active bool) string {
	if active {
		return "Activo"
	}
	return "Inactivo"
}

// search runs the search of Parties with the filters of an export.
func search(ctx context.Context, p *parties.Module, kind, role string, filters map[string]string, cursor string, limit int) (int, int, []parapp.PartyDTO, error) {
	q := parapp.SearchParties{Text: filters["name"], Kind: kind, Role: role, Document: filters["document"], Organization: filters["organization"],
		ActiveOnly: filters["active"] == "true", Page: page(cursor), Size: limit}
	if q.Role == "" {
		q.Role = filters["role"]
	}
	found, err := p.Service.Search.Handle(ctx, q)
	return found.Number, found.TotalPages, found.Items, err
}

var partyFilters = []string{"name", "document", "organization", "active", "role"}

// partyList is a list of parties: name, kind and whether it is active (the C# columns), and for
// customers their tax number, which the C# column had and always left empty.
type partyList struct {
	p          *parties.Module
	key, title string
	kind, role string
	taxID      bool
}

func (l partyList) Key() string                  { return l.key }
func (l partyList) Title() string                { return l.title }
func (l partyList) Permission() authz.Permission { return parapp.PermPartyRead }
func (l partyList) Filters() []string            { return partyFilters }

func (l partyList) Columns() []expdomain.Column {
	cols := []expdomain.Column{{Field: "name", Header: "Nombre"}, {Field: "kind", Header: "Tipo"}}
	if l.taxID {
		cols = append(cols, expdomain.Column{Field: "taxId", Header: "CIF/NIF"})
	}
	return append(cols, expdomain.Column{Field: "status", Header: "Estado"})
}

func (l partyList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	number, total, items, err := search(ctx, l.p, l.kind, l.role, filters, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	taxes := map[string]parcontracts.TaxIdentity{}
	if l.taxID {
		ids := make([]string, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		for len(ids) > 0 {
			n := min(len(ids), parcontracts.MaxDirectoryBatch)
			some, err := l.p.TaxIdentities.TaxIdentities(ctx, ids[:n])
			if err != nil {
				return nil, "", err
			}
			for id, t := range some {
				taxes[id] = t
			}
			ids = ids[n:]
		}
	}
	rows := make([]expdomain.Row, 0, len(items))
	for _, it := range items {
		rows = append(rows, expdomain.Row{"name": it.Name, "kind": kindName(it.Kind), "taxId": taxes[it.ID].Number, "status": status(it.Active)})
	}
	return rows, next(number, total), nil
}

// partyRoles is the list of the roles the parties play: a row per role.
type partyRoles struct{ p *parties.Module }

func (partyRoles) Key() string                  { return "party-roles" }
func (partyRoles) Title() string                { return "Roles de participante" }
func (partyRoles) Permission() authz.Permission { return parapp.PermPartyRead }
func (partyRoles) Filters() []string            { return partyFilters }
func (partyRoles) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "party", Header: "Participante"}, {Field: "role", Header: "Tipo de rol"},
		{Field: "from", Header: "Fecha inicio", Type: expdomain.Date}, {Field: "until", Header: "Fecha expiración", Type: expdomain.Date},
		{Field: "active", Header: "Activo", Type: expdomain.Boolean}}
}

func (l partyRoles) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	number, total, items, err := search(ctx, l.p, "", "", filters, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	rows := []expdomain.Row{}
	for _, it := range items {
		for _, r := range it.Roles {
			row := expdomain.Row{"party": it.Name, "role": r.Name, "from": r.From, "active": r.Active}
			if r.Until != nil {
				row["until"] = *r.Until
			}
			rows = append(rows, row)
		}
	}
	return rows, next(number, total), nil
}

// partyRelationships is the list of the relationships between parties: each once, from the side it
// starts at. Parties has no search of relationships, so they are read party by party; a file of
// them takes longer than the others.
type partyRelationships struct{ p *parties.Module }

func (partyRelationships) Key() string                  { return "party-relationships" }
func (partyRelationships) Title() string                { return "Relaciones" }
func (partyRelationships) Permission() authz.Permission { return parapp.PermRelationshipRead }
func (partyRelationships) Filters() []string            { return partyFilters }
func (partyRelationships) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "type", Header: "Tipo de relación"}, {Field: "from", Header: "Participante origen"},
		{Field: "to", Header: "Participante destino"}, {Field: "since", Header: "Fecha inicio", Type: expdomain.Date},
		{Field: "until", Header: "Fecha expiración", Type: expdomain.Date}, {Field: "status", Header: "Estado"}, {Field: "remark", Header: "Observaciones"}}
}

func (l partyRelationships) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	// A hundred parties a page: each brings its relationships.
	number, total, items, err := search(ctx, l.p, "", "", filters, cursor, min(limit, 100))
	if err != nil {
		return nil, "", err
	}
	type line struct {
		rel  parapp.RelationshipDTO
		from string
	}
	lines, others := []line{}, []string{}
	for _, it := range items {
		id, _ := pardomain.ParsePartyID(it.ID)
		rels, err := l.p.Service.Relationships.Handle(ctx, parapp.PartyRelationships{PartyID: id})
		if err != nil {
			return nil, "", err
		}
		for _, r := range rels {
			if r.FromParty == it.ID {
				lines = append(lines, line{rel: r, from: it.Name})
				others = append(others, r.ToParty)
			}
		}
	}
	names := map[string]parcontracts.PartyRef{}
	for len(others) > 0 {
		n := min(len(others), parcontracts.MaxDirectoryBatch)
		some, err := l.p.Directory.Resolve(ctx, others[:n])
		if err != nil {
			return nil, "", err
		}
		for id, ref := range some {
			names[id] = ref
		}
		others = others[n:]
	}
	rows := make([]expdomain.Row, 0, len(lines))
	for _, ln := range lines {
		row := expdomain.Row{"type": ln.rel.TypeName, "from": ln.from, "to": names[ln.rel.ToParty].Name, "since": ln.rel.Since,
			"status": status(ln.rel.Active), "remark": ln.rel.Remark}
		if ln.rel.Until != nil {
			row["until"] = *ln.rel.Until
		}
		rows = append(rows, row)
	}
	return rows, next(number, total), nil
}

// employees is the list of the employments of HR, with the name Parties gives each person.
type employees struct {
	p *parties.Module
	h *hr.Module
}

func (employees) Key() string                  { return "employees" }
func (employees) Title() string                { return "Empleados" }
func (employees) Permission() authz.Permission { return hrapp.PermEmploymentRead }
func (employees) Filters() []string            { return []string{"organization", "number", "active"} }
func (employees) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "name", Header: "Nombre"}, {Field: "number", Header: "Número empleado"},
		{Field: "hired", Header: "Fecha contratación", Type: expdomain.Date}, {Field: "terminated", Header: "Fecha baja", Type: expdomain.Date},
		{Field: "active", Header: "Activo", Type: expdomain.Boolean}}
}

func (l employees) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.h.Service.SearchEmployments.Handle(ctx, hrapp.SearchEmployments{Employer: filters["organization"], Number: filters["number"],
		ActiveOnly: filters["active"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(found.Items))
	for _, e := range found.Items {
		ids = append(ids, e.Person)
	}
	names := map[string]parcontracts.PartyRef{}
	for len(ids) > 0 {
		n := min(len(ids), parcontracts.MaxDirectoryBatch)
		some, err := l.p.Directory.Resolve(ctx, ids[:n])
		if err != nil {
			return nil, "", err
		}
		for id, ref := range some {
			names[id] = ref
		}
		ids = ids[n:]
	}
	rows := make([]expdomain.Row, 0, len(found.Items))
	for _, e := range found.Items {
		rows = append(rows, expdomain.Row{"name": names[e.Person].Name, "number": e.Number, "hired": e.Hired, "terminated": e.Terminated,
			"active": e.Terminated == ""})
	}
	return rows, next(found.Number, found.TotalPages), nil
}
