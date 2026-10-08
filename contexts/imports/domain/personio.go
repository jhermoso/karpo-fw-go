package domain

import (
	"strconv"
	"strings"
)

// Kinds of record of the organization of a company: the neutral shape every HR source maps to.
const (
	KindLegalEntity = "legal-entity"
	KindDepartment  = "department"
	KindWorkCenter  = "work-center"
	KindPerson      = "person"
	KindEmployment  = "employment"
)

// Roles of the files of Personio.
const (
	RoleOrgUnits = "org-units"
	RolePeople   = "people"
)

// Personio reads the two files exported from Personio (the C# PersonioCsvReader and
// PersonioMapper): the units of the organization and its people.
//
//	org units: unitType,name,parentOrg,notes
//	people:    employeeNumber,firstName,lastName,preferredName,email,gender,legalEntity,department,
//	           workCenter,jobTitle,isSupervisor,notes[,hireDate,terminationDate]
type Personio struct{}

// Key implements Source.
func (Personio) Key() string { return "personio" }

// Files implements Source.
func (Personio) Files() []FileSpec {
	return []FileSpec{{Role: RoleOrgUnits, Required: true, About: "unitType,name,parentOrg,notes"},
		{Role: RolePeople, About: "employeeNumber,firstName,lastName,preferredName,email,gender,legalEntity,department,workCenter,jobTitle,isSupervisor,notes,hireDate,terminationDate"}}
}

// Kinds implements Source.
func (Personio) Kinds() []string {
	return []string{KindLegalEntity, KindDepartment, KindWorkCenter, KindPerson, KindEmployment}
}

type personioRun struct {
	records  []Record
	messages []Message
	legal    []string          // declared legal entities, as written
	folded   map[string]string // folded name → as written
	units    map[string]bool
}

func (p *personioRun) reject(t *Table, r Row, kind, key, code, text string) {
	p.messages = append(p.messages, Message{Severity: SeverityError, Code: code, Text: text, File: t.File, Line: r.Line, Kind: kind, Key: key})
}

func (p *personioRun) warn(t *Table, r Row, kind, key, code, text string) {
	p.messages = append(p.messages, Message{Severity: SeverityWarning, Code: code, Text: text, File: t.File, Line: r.Line, Kind: kind, Key: key})
}

// entity resolves the name of a legal entity among the declared ones: the same name, or the only
// one that starts with it.
func (p *personioRun) entity(name string) (string, bool) {
	f := Fold(name)
	if f == "" {
		return "", false
	}
	if n, ok := p.folded[f]; ok {
		return n, true
	}
	found := ""
	for k, n := range p.folded {
		if strings.HasPrefix(k, f) {
			if found != "" {
				return "", false
			}
			found = n
		}
	}
	return found, found != ""
}

// entities resolves the legal entities of a person. Someone employed by several at once comes as
// "A / B (PLURIEMPLEO)": that is not an entity, it is one employment in each.
func (p *personioRun) entities(written string) ([]string, string) {
	if n, ok := p.entity(written); ok {
		return []string{n}, ""
	}
	if !strings.Contains(written, "/") {
		return nil, written
	}
	if i := strings.LastIndex(written, "("); i > 0 && strings.HasSuffix(strings.TrimSpace(written), ")") {
		written = written[:i]
	}
	out := []string{}
	for _, part := range strings.Split(written, "/") {
		n, ok := p.entity(part)
		if !ok {
			return nil, strings.TrimSpace(part)
		}
		for _, o := range out {
			if o == n {
				return nil, strings.TrimSpace(part)
			}
		}
		out = append(out, n)
	}
	return out, ""
}

func (p *personioRun) orgUnits(t *Table) {
	// Legal entities first: the other units name them as their parent.
	for _, r := range t.Rows {
		if Fold(r.Get("unitType")) != "internalorganization" {
			continue
		}
		name := strings.Join(strings.Fields(r.Get("name")), " ")
		switch {
		case name == "":
			p.reject(t, r, KindLegalEntity, "", "personio.unit_without_name", "the unit has no name")
		case p.folded[Fold(name)] != "":
			p.warn(t, r, KindLegalEntity, name, "personio.duplicate_unit", "the legal entity is declared twice; the second is ignored")
		default:
			p.folded[Fold(name)] = name
			p.legal = append(p.legal, name)
			p.records = append(p.records, Record{Kind: KindLegalEntity, Scope: GlobalScope, Key: name, File: t.File, Line: r.Line,
				Fields: map[string]string{"name": name, "notes": r.Get("notes")}})
		}
	}
	for _, r := range t.Rows {
		kind := KindWorkCenter
		switch Fold(r.Get("unitType")) {
		case "internalorganization":
			continue
		case "department":
			kind = KindDepartment
		case "":
			p.reject(t, r, "", r.Get("name"), "personio.unit_without_type", "the unit has no type")
			continue
		}
		name := strings.Join(strings.Fields(r.Get("name")), " ")
		if name == "" {
			p.reject(t, r, kind, "", "personio.unit_without_name", "the unit has no name")
			continue
		}
		parent, ok := "", true
		if written := r.Get("parentOrg"); written != "" {
			parent, ok = p.entity(written)
		} else if len(p.legal) == 1 {
			parent = p.legal[0] // with one company there is no doubt whose the unit is
		} else {
			ok = kind == KindWorkCenter // a work center may belong to nobody; a department may not
		}
		if !ok {
			p.reject(t, r, kind, name, "personio.unknown_legal_entity", "the unit belongs to a legal entity the file does not declare: "+r.Get("parentOrg"))
			continue
		}
		scope := parent
		if scope == "" {
			scope = GlobalScope
		}
		id := kind + "|" + Fold(scope) + "|" + Fold(name)
		if p.units[id] {
			p.warn(t, r, kind, name, "personio.duplicate_unit", "the unit is declared twice; the second is ignored")
			continue
		}
		p.units[id] = true
		p.records = append(p.records, Record{Kind: kind, Scope: scope, Key: name, File: t.File, Line: r.Line,
			Fields: map[string]string{"name": name, "legalEntity": parent, "notes": r.Get("notes")}})
	}
}

func (p *personioRun) people(t *Table) {
	seen := map[string]bool{}
	for _, r := range t.Rows {
		number := r.Get("employeeNumber")
		if number == "" {
			p.reject(t, r, KindPerson, "", "personio.person_without_number", "the person has no employee number")
			continue
		}
		if seen[number] {
			p.reject(t, r, KindPerson, number, "personio.duplicate_person", "the employee number appears twice; the second is ignored")
			continue
		}
		first, last, preferred := r.Get("firstName"), r.Get("lastName"), r.Get("preferredName")
		full := strings.TrimSpace(first + " " + last)
		if preferred != "" {
			full = preferred
		}
		if full == "" {
			p.reject(t, r, KindPerson, number, "personio.person_without_name", "the person has no name")
			continue
		}
		entities, unknown := p.entities(r.Get("legalEntity"))
		if len(entities) == 0 {
			p.reject(t, r, KindPerson, number, "personio.unknown_legal_entity", "the person works for a legal entity the file does not declare: "+unknown)
			continue
		}
		hired, okHired := Day(r.Get("hireDate", "fechaAlta", "Fecha de contratación"))
		left, okLeft := Day(r.Get("terminationDate", "fechaBaja", "Fecha de baja"))
		if !okHired || !okLeft || (hired != "" && left != "" && left < hired) {
			p.reject(t, r, KindPerson, number, "personio.dates", "the hire and termination dates cannot be read, or the person left before arriving")
			continue
		}
		seen[number] = true
		p.records = append(p.records, Record{Kind: KindPerson, Scope: GlobalScope, Key: number, File: t.File, Line: r.Line, Fields: map[string]string{
			"employeeNumber": number, "firstName": first, "lastName": last, "preferredName": preferred, "fullName": full,
			"email": strings.ToLower(r.Get("email")), "gender": r.Get("gender"), "notes": r.Get("notes")}})
		for i, e := range entities {
			f := map[string]string{"employeeNumber": number, "legalEntity": e, "primary": strconv.FormatBool(i == 0), "hireDate": hired, "terminationDate": left,
				"department": "", "workCenter": "", "jobTitle": "", "supervisor": "false"}
			if i == 0 { // only the first employment keeps the department, the work center and the job
				f["department"], f["workCenter"], f["jobTitle"] = r.Get("department"), r.Get("workCenter"), r.Get("jobTitle")
				f["supervisor"] = strconv.FormatBool(Truth(r.Get("isSupervisor")))
			}
			p.records = append(p.records, Record{Kind: KindEmployment, Scope: e, Key: number, File: t.File, Line: r.Line, Fields: f})
		}
	}
}

// Read implements Source.
func (Personio) Read(files []File) ([]Record, []Message, error) {
	p := &personioRun{folded: map[string]string{}, units: map[string]bool{}}
	for _, role := range []string{RoleOrgUnits, RolePeople} {
		for _, f := range files {
			if f.Role != role {
				continue
			}
			t, err := ParseTable(f, ',')
			if err != nil {
				return nil, nil, err
			}
			if role == RoleOrgUnits {
				p.orgUnits(t)
			} else {
				p.people(t)
			}
		}
	}
	return p.records, p.messages, nil
}
