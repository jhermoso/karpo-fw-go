// Package contracts is what other bounded contexts may depend on: the Published Language of
// Human Resources and its query ports (Payroll reads the primary contract of an employee;
// WorkEffort and approvals read who holds a position and who supervises it).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "hr"

// MaxBatch is the maximum number of ids per call.
const MaxBatch = 900

// ContractRef is the primary labor contract of an employment on a date.
type ContractRef struct {
	ID          string `json:"id"`
	TypeCode    string `json:"typeCode"`
	Start       string `json:"start"`         // civil date
	End         string `json:"end,omitempty"` // civil date
	Agreement   string `json:"agreement"`
	WorkCenter  string `json:"workCenter"`
	WeeklyHours string `json:"weeklyHours,omitempty"`
}

// EmploymentRef is an employment of a person.
type EmploymentRef struct {
	ID          string       `json:"id"`
	Person      string       `json:"person"`
	Employer    string       `json:"employer"`
	Number      string       `json:"number,omitempty"`
	Hired       string       `json:"hired"`
	Terminated  string       `json:"terminated,omitempty"`
	JobCategory string       `json:"jobCategory,omitempty"`
	WageGroup   string       `json:"wageGroup,omitempty"`
	Contract    *ContractRef `json:"contract,omitempty"` // the primary contract on the date asked
}

// Staff answers who is employed (what the C# Payroll read from the Employee role and the
// LaborContract tables). Missing people are absent; date is a civil date (YYYY-MM-DD).
type Staff interface {
	EmploymentsOn(ctx context.Context, personIDs []string, date string) (map[string][]EmploymentRef, error)
}

// PositionRef is a position with its current holder and primary supervisor position.
type PositionRef struct {
	ID           string `json:"id"`
	Unit         string `json:"unit"`
	Organization string `json:"organization"`
	Type         string `json:"type"`
	Holder       string `json:"holder,omitempty"`
	ReportsTo    string `json:"reportsTo,omitempty"`
	Active       bool   `json:"active"`
}

// Positions resolves positions in batches (holders and supervisors now). Missing positions are
// absent.
type Positions interface {
	Resolve(ctx context.Context, ids []string) (map[string]PositionRef, error)
	HeldBy(ctx context.Context, personID string) ([]PositionRef, error)
}

// EmployeeHiredV1 is published when a person is hired.
type EmployeeHiredV1 struct {
	EmploymentID string `json:"employmentId"`
	Person       string `json:"person"`
	Employer     string `json:"employer"`
	Number       string `json:"number,omitempty"`
	Hired        string `json:"hired"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (EmployeeHiredV1) IntegrationEventType() string { return "hr.employee-hired.v1" }

// EmployeeTerminatedV1 is published when an employment ends.
type EmployeeTerminatedV1 struct {
	EmploymentID string `json:"employmentId"`
	Person       string `json:"person"`
	Employer     string `json:"employer"`
	Terminated   string `json:"terminated"`
	Reason       string `json:"reason,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (EmployeeTerminatedV1) IntegrationEventType() string { return "hr.employee-terminated.v1" }

// ContractStartedV1 is published when a labor contract is added.
type ContractStartedV1 struct {
	EmploymentID string `json:"employmentId"`
	ContractID   string `json:"contractId"`
	TypeCode     string `json:"typeCode"`
	Start        string `json:"start"`
	Agreement    string `json:"agreement"`
	WorkCenter   string `json:"workCenter"`
	Primary      bool   `json:"primary"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ContractStartedV1) IntegrationEventType() string { return "hr.contract-started.v1" }

// ContractEndedV1 is published when a labor contract ends.
type ContractEndedV1 struct {
	EmploymentID string `json:"employmentId"`
	ContractID   string `json:"contractId"`
	End          string `json:"end"`
	Reason       string `json:"reason,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ContractEndedV1) IntegrationEventType() string { return "hr.contract-ended.v1" }

// PositionFilledV1 is published when a person takes a position.
type PositionFilledV1 struct {
	PositionID string `json:"positionId"`
	Person     string `json:"person"`
	From       string `json:"from"` // RFC 3339
}

// IntegrationEventType implements application.IntegrationEvent.
func (PositionFilledV1) IntegrationEventType() string { return "hr.position-filled.v1" }

// PositionVacatedV1 is published when the holder leaves a position.
type PositionVacatedV1 struct {
	PositionID string `json:"positionId"`
	Person     string `json:"person"`
	At         string `json:"at"` // RFC 3339
}

// IntegrationEventType implements application.IntegrationEvent.
func (PositionVacatedV1) IntegrationEventType() string { return "hr.position-vacated.v1" }
