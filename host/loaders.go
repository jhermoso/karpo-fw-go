package host

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	facapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	facdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hrdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	impdomain "github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	parcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The loaders of an import of the organization of a company (the kinds of the Personio source).
// Each writes with the use cases of the context that owns what it loads, as who runs the import:
// their permissions and their rules apply. What exists is left as it is: an import fills in, it
// does not overwrite what somebody keeps by hand.

// companyOf returns the company a record belongs to, by the name the source gives it: one the
// import brought or found before (its reference).
func companyOf(refs impdomain.Refs, name string) (string, error) {
	if id, ok := refs.Lookup(impdomain.KindLegalEntity, impdomain.GlobalScope, name); ok && id != "" {
		return id, nil
	}
	return "", fw.Violation("imports.unknown_company", "the company "+name+" was not loaded: what belongs to it cannot be either")
}

// Departments loads the departments of a company into Parties: an organization with the role of
// department, rolled up to its company.
type Departments struct {
	Parties *parties.Module
	UoW     fw.UnitOfWork
}

// Kind implements imports' Loader.
func (Departments) Kind() string { return impdomain.KindDepartment }

// EntityType implements imports' Loader.
func (Departments) EntityType() string { return pardomain.PartyKind }

// Find looks for a unit of that name under the company.
func (l Departments) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", nil // nothing to find under a company that is not there: Apply will say so
	}
	below, err := l.Parties.Organizations.Descendants(ctx, []string{company})
	if err != nil {
		return "", err
	}
	name := impdomain.Fold(r.Fields["name"])
	for len(below) > 0 {
		n := min(len(below), parcontracts.MaxDirectoryBatch)
		found, err := l.Parties.Directory.Resolve(ctx, below[:n])
		if err != nil {
			return "", err
		}
		for id, p := range found {
			if id != company && impdomain.Fold(p.Name) == name {
				return id, nil
			}
		}
		below = below[n:]
	}
	return "", nil
}

// Apply registers the department and rolls it up to its company, both or neither.
func (l Departments) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", "", err
	}
	id := ""
	err = l.UoW.Do(ctx, func(ctx context.Context) error {
		d, err := l.Parties.Service.RegisterOrganization.Handle(ctx, parapp.RegisterOrganization{LegalName: r.Fields["name"],
			Roles: []string{pardomain.RoleDepartment.String()}})
		if err != nil {
			return err
		}
		id = d.ID
		_, err = l.Parties.Service.EstablishRelationship.Handle(ctx, parapp.EstablishRelationship{Type: pardomain.RelOrganizationRollup.String(),
			From: d.ID, To: company})
		return err
	})
	if err != nil {
		return "", "", err
	}
	return id, impdomain.Created, nil
}

// WorkCenters loads the work centers of a company into Facilities, as buildings: the file brings
// a name and nothing else, and an office needs an address and a phone. Making one a work center
// of HR (its Social Security code, the day it opened) is done by hand: the file does not say.
type WorkCenters struct{ Facilities *facilities.Module }

// Kind implements imports' Loader.
func (WorkCenters) Kind() string { return impdomain.KindWorkCenter }

// EntityType implements imports' Loader.
func (WorkCenters) EntityType() string { return facdomain.FacilityKind }

func (l WorkCenters) company(r impdomain.Record, refs impdomain.Refs) (string, error) {
	if r.Fields["legalEntity"] == "" {
		return "", fw.Violation("imports.work_center_without_company", "a work center belongs to a company: the file gives none for this one")
	}
	return companyOf(refs, r.Fields["legalEntity"])
}

// Find looks for a facility of that name in the company.
func (l WorkCenters) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	company, err := l.company(r, refs)
	if err != nil {
		return "", nil
	}
	name := impdomain.Fold(r.Fields["name"])
	for page := 1; ; page++ {
		found, err := l.Facilities.Service.Search.Handle(ctx, facapp.SearchFacilities{Text: r.Fields["name"], Organization: company, Page: page, Size: 100})
		if err != nil {
			return "", err
		}
		for _, f := range found.Items {
			if impdomain.Fold(f.Name) == name {
				return f.ID, nil
			}
		}
		if page >= found.TotalPages {
			return "", nil
		}
	}
}

// Apply registers the facility.
func (l WorkCenters) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	company, err := l.company(r, refs)
	if err != nil {
		return "", "", err
	}
	f, err := l.Facilities.Service.Register.Handle(ctx, facapp.RegisterFacility{Organization: company, Type: facdomain.TypeBuilding.String(),
		Name: r.Fields["name"], Description: r.Fields["notes"]})
	if err != nil {
		return "", "", err
	}
	return f.ID, impdomain.Created, nil
}

// People loads the persons of an import into Parties, affiliated as employees with the first
// company they work for, with their email.
type People struct {
	Parties *parties.Module
	UoW     fw.UnitOfWork
}

// Kind implements imports' Loader.
func (People) Kind() string { return impdomain.KindPerson }

// EntityType implements imports' Loader.
func (People) EntityType() string { return pardomain.PartyKind }

// names returns the given name and the surname of a record: those the file gives, or the full
// name cut at its first space when it gives only that.
func names(r impdomain.Record) (string, string) {
	given, surname := strings.TrimSpace(r.Fields["firstName"]), strings.TrimSpace(r.Fields["lastName"])
	if given == "" || surname == "" {
		given, surname, _ = strings.Cut(strings.TrimSpace(r.Fields["fullName"]), " ")
	}
	return given, strings.TrimSpace(surname)
}

func gender(s string) string {
	switch impdomain.Fold(s) {
	case "f", "female", "mujer", "femenino":
		return string(pardomain.GenderFemale)
	case "m", "male", "hombre", "masculino", "h":
		return string(pardomain.GenderMale)
	case "", "-":
		return ""
	}
	return string(pardomain.GenderOther)
}

// Find looks for a person of that name among those of the company. (The key of the source, once
// linked, is what tells two people of the same name apart.)
func (l People) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	company, err := companyOf(refs, r.Fields["legalEntity"])
	if err != nil {
		return "", nil
	}
	given, surname := names(r)
	name := impdomain.Fold(given + " " + surname)
	found, err := l.Parties.Service.Search.Handle(ctx, parapp.SearchParties{Text: given + " " + surname, Kind: string(pardomain.KindPerson),
		Organization: company, Size: 100})
	if err != nil {
		return "", err
	}
	match := ""
	for _, p := range found.Items {
		if impdomain.Fold(p.Name) == name {
			if match != "" {
				return "", nil // two of that name: nobody can tell which; a new one is not what is wanted either
			}
			match = p.ID
		}
	}
	return match, nil
}

// Apply registers the person with their affiliation and their email, all or nothing.
func (l People) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	company, err := companyOf(refs, r.Fields["legalEntity"])
	if err != nil {
		return "", "", err
	}
	given, surname := names(r)
	id := ""
	err = l.UoW.Do(ctx, func(ctx context.Context) error {
		p, err := l.Parties.Service.RegisterPerson.Handle(ctx, parapp.RegisterPerson{GivenName: given, FirstSurname: surname, Gender: gender(r.Fields["gender"]),
			Affiliation: &parapp.NewAffiliation{Organization: company, RelationshipType: pardomain.RelEmployment.String()}})
		if err != nil {
			return err
		}
		id = p.ID
		if email := r.Fields["email"]; email != "" {
			pid, _ := pardomain.ParsePartyID(p.ID)
			_, err = l.Parties.Service.AddContact.Handle(ctx, parapp.AddContact{PartyID: pid, Kind: string(pardomain.ContactEmail), Value: email,
				Purposes: []string{"default"}})
		}
		return err
	})
	if err != nil {
		return "", "", err
	}
	return id, impdomain.Created, nil
}

// Employments loads into HR who works for which company, since when and until when. Department,
// work center and job are not loaded yet: in HR they belong to the contract and the position,
// which need what the file does not bring (type of contract, collective agreement).
type Employments struct {
	HR      *hr.Module
	Parties *parties.Module
	UoW     fw.UnitOfWork
}

// Kind implements imports' Loader.
func (Employments) Kind() string { return impdomain.KindEmployment }

// EntityType implements imports' Loader.
func (Employments) EntityType() string { return hrdomain.EmploymentKind }

// Find looks for the employment of that number in the company.
func (l Employments) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	employer, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", nil
	}
	found, err := l.HR.Service.SearchEmployments.Handle(ctx, hrapp.SearchEmployments{Employer: employer, Number: r.Key, Size: 5})
	if err != nil || len(found.Items) == 0 {
		return "", err
	}
	return found.Items[0].ID, nil
}

func day(s string) (vocab.Date, bool) {
	d, err := vocab.ParseDate(s)
	return d, err == nil && s != ""
}

// Apply hires the person (affiliating them with the company first when it is not the one they were
// registered with) and ends the employment when the file gives the day. An employment that exists
// is only ended, if the file now says it ended.
func (l Employments) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	left, hasLeft := day(r.Fields["terminationDate"])
	if existing != "" {
		id, err := hrdomain.ParseEmploymentID(existing)
		if err != nil {
			return "", "", err
		}
		cur, err := l.HR.Service.GetEmployment.Handle(ctx, hrapp.GetEmployment{ID: id})
		if err != nil {
			return "", "", err
		}
		if !hasLeft || cur.Terminated != "" {
			return existing, impdomain.Unchanged, nil
		}
		if _, err := l.HR.Service.Terminate.Handle(ctx, hrapp.Terminate{ID: id, On: left, Reason: "import"}); err != nil {
			return "", "", err
		}
		return existing, impdomain.Updated, nil
	}
	employer, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", "", err
	}
	person, ok := refs.Lookup(impdomain.KindPerson, impdomain.GlobalScope, r.Key)
	if !ok || person == "" {
		return "", "", fw.Violation("imports.unknown_person", "the person "+r.Key+" was not loaded: their employment cannot be either")
	}
	hired, ok := day(r.Fields["hireDate"])
	if !ok {
		return "", "", fw.Violation("imports.hire_date_required", "HR needs the day the person was hired, and the file does not give it")
	}
	id := ""
	err = l.UoW.Do(ctx, func(ctx context.Context) error {
		hire := hrapp.Hire{Person: person, Employer: employer, Number: r.Key, Hired: hired}
		e, err := l.HR.Service.Hire.Handle(ctx, hire)
		var rule *fw.RuleViolationError
		if errors.As(err, &rule) && rule.Code == "hr.not_affiliated" {
			// Someone who works for a second company: Parties is told first, and that they are
			// an employee if it does not know yet.
			pid, _ := pardomain.ParsePartyID(person)
			p, gerr := l.Parties.Service.Get.Handle(ctx, parapp.GetParty{ID: pid})
			if gerr != nil {
				return gerr
			}
			employee := false
			for _, role := range p.Roles {
				employee = employee || role.Active && role.RoleType == pardomain.RoleEmployee.String()
			}
			if !employee {
				if _, err := l.Parties.Service.AssignRole.Handle(ctx, parapp.AssignRole{PartyID: pid, RoleType: pardomain.RoleEmployee.String()}); err != nil {
					return err
				}
			}
			// From today, as the affiliation of who is registered: the dates of the job are HR's.
			if _, err := l.Parties.Service.EstablishRelationship.Handle(ctx, parapp.EstablishRelationship{Type: pardomain.RelEmployment.String(),
				From: person, To: employer}); err != nil {
				return err
			}
			e, err = l.HR.Service.Hire.Handle(ctx, hire)
		}
		if err != nil {
			return err
		}
		id = e.ID
		if hasLeft {
			eid, _ := hrdomain.ParseEmploymentID(e.ID)
			_, err = l.HR.Service.Terminate.Handle(ctx, hrapp.Terminate{ID: eid, On: left, Reason: "import"})
		}
		return err
	})
	if err != nil {
		return "", "", err
	}
	return id, impdomain.Created, nil
}

// moment returns the instant a day of a file begins, or nil when the file gives none (then: now).
func moment(s string) *time.Time {
	d, ok := day(s)
	if !ok {
		return nil
	}
	t := d.BaseTime()
	return &t
}

// employee returns the person of an employee number an import loaded.
func employee(refs impdomain.Refs, number string) (string, error) {
	if id, ok := refs.Lookup(impdomain.KindPerson, impdomain.GlobalScope, number); ok && id != "" {
		return id, nil
	}
	return "", fw.Violation("imports.unknown_person", "the person "+number+" was not loaded: what is theirs cannot be either")
}

// Positions loads into HR the job each person holds: a position of the type the job title names,
// in their department (or in the company when the file gives none), held by them since they were
// hired. That is how HR knows which department someone is in.
type Positions struct {
	HR  *hr.Module
	UoW fw.UnitOfWork
}

// Kind implements imports' Loader.
func (Positions) Kind() string { return impdomain.KindPosition }

// EntityType implements imports' Loader.
func (Positions) EntityType() string { return hrdomain.PositionKind }

// unit returns where the position is: the department of the record, or its company.
func (l Positions) unit(r impdomain.Record, refs impdomain.Refs) (string, error) {
	if name := r.Fields["department"]; name != "" {
		if id, ok := refs.Lookup(impdomain.KindDepartment, r.Scope, name); ok && id != "" {
			return id, nil
		}
		return "", fw.Violation("imports.unknown_department", "the department "+name+" of "+r.Scope+" was not loaded")
	}
	return companyOf(refs, r.Scope)
}

// jobType returns the type of position a job title names: the one of the catalog of HR with that
// title, without minding case or accents.
func (l Positions) jobType(ctx context.Context, title string) (string, error) {
	types, err := l.HR.Service.PositionTypes.Handle(ctx, hrapp.ListPositionTypes{})
	if err != nil {
		return "", err
	}
	want := impdomain.Fold(title)
	for _, t := range types {
		if t.Active && impdomain.Fold(t.Title) == want {
			return t.ID.String(), nil
		}
	}
	return "", fw.Violation("imports.unknown_job_title", "no type of position of HR is called "+title+": add it to the catalog or correct the file")
}

// Find looks for a position of that type in that unit the person already holds.
func (l Positions) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	unit, err1 := l.unit(r, refs)
	person, err2 := employee(refs, r.Key)
	typ, err3 := l.jobType(ctx, r.Fields["jobTitle"])
	if err1 != nil || err2 != nil || err3 != nil {
		return "", nil // Apply will say what is missing
	}
	found, err := l.HR.Service.SearchPositions.Handle(ctx, hrapp.SearchPositions{Unit: unit, Type: typ, Holder: person, Size: 5})
	if err != nil || len(found.Items) == 0 {
		return "", err
	}
	return found.Items[0].ID, nil
}

// Apply opens the position and gives it to the person, both or neither.
func (l Positions) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	unit, err := l.unit(r, refs)
	if err != nil {
		return "", "", err
	}
	person, err := employee(refs, r.Key)
	if err != nil {
		return "", "", err
	}
	typ, err := l.jobType(ctx, r.Fields["jobTitle"])
	if err != nil {
		return "", "", err
	}
	from, id := moment(r.Fields["from"]), ""
	err = l.UoW.Do(ctx, func(ctx context.Context) error {
		p, err := l.HR.Service.OpenPosition.Handle(ctx, hrapp.OpenPosition{Unit: unit, Type: typ, PlannedFrom: from, FullTime: true})
		if err != nil {
			return err
		}
		id = p.ID
		pid, _ := hrdomain.ParsePositionID(p.ID)
		_, err = l.HR.Service.FillPosition.Handle(ctx, hrapp.FillPosition{ID: pid, Person: person, From: from})
		return err
	})
	if err != nil {
		return "", "", err
	}
	return id, impdomain.Created, nil
}

// WorkPlaces loads into Parties the work center each person works at: the role of work center
// they play at the facility an import loaded for it.
type WorkPlaces struct{ Parties *parties.Module }

// Kind implements imports' Loader.
func (WorkPlaces) Kind() string { return impdomain.KindWorkPlace }

// EntityType implements imports' Loader.
func (WorkPlaces) EntityType() string { return "parties.facility_role" }

func (l WorkPlaces) facility(r impdomain.Record, refs impdomain.Refs) (string, error) {
	name := r.Fields["workCenter"]
	if id, ok := refs.Lookup(impdomain.KindWorkCenter, r.Scope, name); ok && id != "" {
		return id, nil
	}
	return "", fw.Violation("imports.unknown_work_center", "the work center "+name+" of "+r.Scope+" was not loaded")
}

// role returns the role of work center a person plays now at a facility.
func (l WorkPlaces) role(ctx context.Context, person, facility string) (string, error) {
	pid, err := pardomain.ParsePartyID(person)
	if err != nil {
		return "", err
	}
	p, err := l.Parties.Service.Get.Handle(ctx, parapp.GetParty{ID: pid})
	if err != nil {
		return "", err
	}
	for _, fr := range p.FacilityRoles {
		if fr.Active && fr.Facility == facility && fr.RoleType == pardomain.FacilityWorkCenter.String() {
			return fr.ID, nil
		}
	}
	return "", nil
}

// Find looks for the role the person already plays at the work center.
func (l WorkPlaces) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	facility, err1 := l.facility(r, refs)
	person, err2 := employee(refs, r.Key)
	if err1 != nil || err2 != nil {
		return "", nil
	}
	return l.role(ctx, person, facility)
}

// Apply makes the person play the role of work center at the facility.
func (l WorkPlaces) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	facility, err := l.facility(r, refs)
	if err != nil {
		return "", "", err
	}
	person, err := employee(refs, r.Key)
	if err != nil {
		return "", "", err
	}
	pid, _ := pardomain.ParsePartyID(person)
	if _, err := l.Parties.Service.AssignFacilityRole.Handle(ctx, parapp.AssignFacilityRole{PartyID: pid, Facility: facility,
		RoleType: pardomain.FacilityWorkCenter.String(), From: moment(r.Fields["from"])}); err != nil {
		return "", "", err
	}
	id, err := l.role(ctx, person, facility)
	if err != nil || id == "" {
		return "", "", errors.Join(err, errors.New("the role was assigned and cannot be read back"))
	}
	return id, impdomain.Created, nil
}

var (
	_ impdomain.Loader = Positions{}
	_ impdomain.Loader = WorkPlaces{}
	_ impdomain.Loader = Departments{}
	_ impdomain.Loader = WorkCenters{}
	_ impdomain.Loader = People{}
	_ impdomain.Loader = Employments{}
)
