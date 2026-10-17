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

// Las listas de Parties que exporta la rejilla (el ExportService de C# las servía desde un switch
// propio; aquí cada una es un dataset sobre las búsquedas del contexto dueño de la lista). Leen
// como quien pidió el fichero, así que cada fichero tiene lo que esa persona puede ver.

// PartyLists construye las listas de parties: todas, y las de una clase o con un rol.
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

// search ejecuta la búsqueda de Parties con los filtros de una exportación.
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

// partyList es una lista de parties: nombre, clase y si está activa (las columnas de C#), y para
// los clientes su número fiscal, que la columna de C# tenía y siempre dejaba vacío.
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

// partyRoles es la lista de los roles que desempeñan las parties: una fila por rol.
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

// partyRelationships es la lista de las relaciones entre parties: cada una una vez, desde el lado
// en que empieza. Parties no tiene búsqueda de relaciones, así que se leen party a party; un
// fichero de ellas tarda más que los otros.
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
	// Cien parties por página: cada una trae sus relaciones.
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

// employees es la lista de los empleos de HR, con el nombre que Parties da a cada persona.
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
