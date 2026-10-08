package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// EmploymentKind is the stable aggregate type name.
const EmploymentKind = "hr.employment"

// Contract is a labor contract of an employment (child entity; the C# LaborContract lived in
// Parties). Dates are civil dates.
type Contract struct {
	ID                ContractID
	TypeCode          string // official contract type (e.g. 100 indefinido, 401 obra)
	Number            string
	Start             vocab.Date
	End               vocab.Date // zero while open
	Agreement         AgreementID
	WorkCenter        WorkCenterID
	WeeklyHours       vocab.Decimal // zero: full time of the agreement
	Bonified          bool
	Primary           bool
	TerminationReason string
}

// Open reports whether the contract is in force on a date.
func (c Contract) OpenOn(d vocab.Date) bool {
	return !d.Before(c.Start) && (c.End.IsZero() || !d.After(c.End))
}

// Employment is the hiring of a person by an internal organization, with its labor contracts. It
// absorbs the HR data of the C# Employee party role (number, hire and termination dates, job
// category, wage group); Parties keeps the person, the Employee role and the Employment
// relationship that makes the person visible to the employer.
type Employment struct {
	fw.BaseAggregateRoot[EmploymentID]
	traits.Audited
	person      PersonID
	employer    OrganizationID
	number      string
	hired       vocab.Date
	terminated  vocab.Date
	reason      string
	jobCategory string
	wageGroup   string
	contracts   []Contract
}

// EmploymentState is the persisted state of an employment.
type EmploymentState struct {
	Person      PersonID
	Employer    OrganizationID
	Number      string
	Hired       vocab.Date
	Terminated  vocab.Date
	Reason      string
	JobCategory string
	WageGroup   string
	Contracts   []Contract
	Audit       traits.AuditStamp
}

func shortCode(v *fw.Validation, field, s string, max int) string {
	s = strings.TrimSpace(s)
	v.Require(utf8.RuneCountInString(s) <= max, field, "length", "too long")
	return s
}

// ReconstituteEmployment rebuilds an employment.
func ReconstituteEmployment(id EmploymentID, s EmploymentState) (*Employment, error) {
	base, err := fw.NewBaseAggregateRoot(EmploymentKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Person.IsZero(), "person", "required", "an employment needs a person")
	v.Require(!s.Employer.IsZero(), "employer", "required", "an employment needs an employer")
	v.Require(!s.Hired.IsZero(), "hired", "required", "the hire date is required")
	v.Require(s.Terminated.IsZero() || !s.Terminated.Before(s.Hired), "terminated", "order", "termination cannot precede hiring")
	e := &Employment{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), person: s.Person, employer: s.Employer,
		number: shortCode(&v, "number", s.Number, 50), hired: s.Hired, terminated: s.Terminated, reason: shortCode(&v, "reason", s.Reason, 30),
		jobCategory: shortCode(&v, "jobCategory", s.JobCategory, 20), wageGroup: shortCode(&v, "wageGroup", s.WageGroup, 20),
		contracts: slices.Clone(s.Contracts)}
	if err := v.Err(); err != nil {
		return nil, err
	}
	return e, nil
}

// Hire creates an employment. The C# checked the hire date was at most a year ahead: kept.
func Hire(id EmploymentID, s EmploymentState, today vocab.Date) (*Employment, error) {
	if s.Hired.After(today.AddDays(366)) {
		var v fw.Validation
		v.Add("hired", "range", "the hire date is too far in the future")
		return nil, v.Err()
	}
	s.Terminated, s.Reason, s.Contracts = vocab.Date{}, "", nil
	e, err := ReconstituteEmployment(id, s)
	if err != nil {
		return nil, err
	}
	e.Raise(EmployeeHired{EventMeta: e.NewEventMeta(), Person: s.Person.String(), Employer: s.Employer.String(),
		Number: e.number, Hired: s.Hired.String()})
	return e, nil
}

// Person returns the employee.
func (e *Employment) Person() PersonID { return e.person }

// Employer returns the employing internal organization.
func (e *Employment) Employer() OrganizationID { return e.employer }

// Number returns the employee number.
func (e *Employment) Number() string { return e.number }

// Hired returns the hire date.
func (e *Employment) Hired() vocab.Date { return e.hired }

// Terminated returns the termination date (zero while employed).
func (e *Employment) Terminated() vocab.Date { return e.terminated }

// TerminationReason returns the termination reason code.
func (e *Employment) TerminationReason() string { return e.reason }

// JobCategory returns the job category code.
func (e *Employment) JobCategory() string { return e.jobCategory }

// WageGroup returns the wage group code.
func (e *Employment) WageGroup() string { return e.wageGroup }

// Contracts returns a copy of the contracts.
func (e *Employment) Contracts() []Contract { return slices.Clone(e.contracts) }

// ActiveOn reports whether the person is employed on a date.
func (e *Employment) ActiveOn(d vocab.Date) bool {
	return !d.Before(e.hired) && (e.terminated.IsZero() || !d.After(e.terminated))
}

// PrimaryContractOn returns the primary contract in force on a date (what Payroll needs).
func (e *Employment) PrimaryContractOn(d vocab.Date) (Contract, bool) {
	for _, c := range e.contracts {
		if c.Primary && c.OpenOn(d) {
			return c, true
		}
	}
	return Contract{}, false
}

// SetClassification changes the job category and wage group.
func (e *Employment) SetClassification(jobCategory, wageGroup string) error {
	var v fw.Validation
	jc, wg := shortCode(&v, "jobCategory", jobCategory, 20), shortCode(&v, "wageGroup", wageGroup, 20)
	if err := v.Err(); err != nil {
		return err
	}
	e.jobCategory, e.wageGroup = jc, wg
	return nil
}

// AddContract adds a labor contract. Invariants: inside the employment; a valid type code; one
// primary contract in force at a time (a new primary makes the overlapping one secondary). The
// application checks the agreement is in force and the work center belongs to the employer.
func (e *Employment) AddContract(c Contract) (ContractID, error) {
	var v fw.Validation
	c.TypeCode = shortCode(&v, "typeCode", c.TypeCode, 20)
	v.Require(c.TypeCode != "", "typeCode", "required", "the contract type is required")
	c.Number = shortCode(&v, "number", c.Number, 30)
	v.Require(!c.Start.IsZero(), "start", "required", "the start date is required")
	v.Require(c.End.IsZero() || !c.End.Before(c.Start), "end", "order", "the end cannot precede the start")
	v.Require(c.WeeklyHours.Sign() >= 0 && c.WeeklyHours.LessThanOrEqual(vocab.DecimalFromInt(60)), "weeklyHours", "range", "0 to 60 hours")
	if err := v.Err(); err != nil {
		return ContractID{}, err
	}
	if c.Start.Before(e.hired) || (!e.terminated.IsZero() && c.Start.After(e.terminated)) {
		return ContractID{}, fw.Violation("hr.contract_outside_employment", "the contract must start while the person is employed")
	}
	c.ID = ContractID{fw.NewUUID()}
	c.TerminationReason = ""
	contracts := slices.Clone(e.contracts)
	if c.Primary {
		for i, x := range contracts {
			if x.Primary && datesOverlap(x.Start, x.End, c.Start, c.End) {
				contracts[i].Primary = false
			}
		}
	}
	e.contracts = append(contracts, c)
	e.Raise(ContractStarted{EventMeta: e.NewEventMeta(), Contract: c.ID.String(), TypeCode: c.TypeCode, Start: c.Start.String(),
		Agreement: c.Agreement.String(), WorkCenter: c.WorkCenter.String(), Primary: c.Primary})
	return c.ID, nil
}

// EndContract ends a contract on a date (not before it started).
func (e *Employment) EndContract(id ContractID, end vocab.Date, reason string) error {
	i := slices.IndexFunc(e.contracts, func(c Contract) bool { return c.ID == id })
	if i < 0 {
		return fw.NotFound("hr.contract", id)
	}
	c := e.contracts[i]
	if end.Before(c.Start) {
		return fw.Violation("hr.contract_end_before_start", "a contract cannot end before it starts")
	}
	if !c.End.IsZero() && !end.Before(c.End) {
		return nil
	}
	var v fw.Validation
	reason = shortCode(&v, "reason", reason, 30)
	if err := v.Err(); err != nil {
		return err
	}
	e.contracts = slices.Clone(e.contracts)
	e.contracts[i].End, e.contracts[i].TerminationReason = end, reason
	e.Raise(ContractEnded{EventMeta: e.NewEventMeta(), Contract: id.String(), End: end.String(), Reason: reason})
	return nil
}

// Terminate ends the employment on a date and every contract still open (the C# Terminate only
// set a date). The application vacates the positions the person holds.
func (e *Employment) Terminate(on vocab.Date, reason string) error {
	if !e.terminated.IsZero() {
		return fw.Violation("hr.already_terminated", "the employment is already terminated")
	}
	if on.Before(e.hired) {
		return fw.Violation("hr.termination_before_hire", "termination cannot precede hiring")
	}
	var v fw.Validation
	reason = shortCode(&v, "reason", reason, 30)
	if err := v.Err(); err != nil {
		return err
	}
	for _, c := range e.contracts {
		if c.End.IsZero() || c.End.After(on) {
			if err := e.EndContract(c.ID, on, reason); err != nil {
				return err
			}
		}
	}
	e.terminated, e.reason = on, reason
	e.Raise(EmployeeTerminated{EventMeta: e.NewEventMeta(), Person: e.person.String(), Employer: e.employer.String(),
		Terminated: on.String(), Reason: reason})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (e *Employment) AuditSnapshot() map[string]any {
	return map[string]any{"number": e.number, "hired": e.hired.String(), "terminated": e.terminated.String(),
		"jobCategory": e.jobCategory, "wageGroup": e.wageGroup, "contracts": len(e.contracts)}
}

func datesOverlap(aStart, aEnd, bStart, bEnd vocab.Date) bool {
	return (aEnd.IsZero() || !bStart.After(aEnd)) && (bEnd.IsZero() || !aStart.After(bEnd))
}

// Employment fields and specifications.
var (
	EmpFieldPerson          = spec.Comparable("person", (*Employment).Person)
	EmpFieldEmployer        = spec.Comparable("employer", (*Employment).Employer)
	EmpFieldNumber          = spec.Ordered("employee_number", (*Employment).Number)
	EmpFieldEnded           = spec.Comparable("terminated_flag", func(e *Employment) bool { return !e.terminated.IsZero() })
	EmpFieldContracts       = spec.Collection("contracts", (*Employment).Contracts)
	ContractFieldWorkCenter = spec.Comparable("work_center", func(c Contract) WorkCenterID { return c.WorkCenter })
	EmpFieldID              = spec.Comparable("id", func(e *Employment) EmploymentID { return e.ID() })
)

// CurrentEmploymentsOf matches the open employments of a person.
func CurrentEmploymentsOf(p PersonID) spec.Spec[*Employment] {
	return EmpFieldPerson.Eq(p).And(EmpFieldEnded.Eq(false))
}
