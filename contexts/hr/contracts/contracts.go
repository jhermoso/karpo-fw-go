// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Recursos Humanos y sus puertos de consulta (Payroll lee el contrato principal de un
// empleado; WorkEffort y las aprobaciones leen quién ocupa un puesto y quién lo supervisa).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "hr"

// MaxBatch es el número máximo de ids por llamada.
const MaxBatch = 900

// ContractRef es el contrato de trabajo principal de una relación laboral en una fecha.
type ContractRef struct {
	ID          string `json:"id"`
	TypeCode    string `json:"typeCode"`
	Start       string `json:"start"`         // fecha civil
	End         string `json:"end,omitempty"` // fecha civil
	Agreement   string `json:"agreement"`
	WorkCenter  string `json:"workCenter"`
	WeeklyHours string `json:"weeklyHours,omitempty"`
}

// EmploymentRef es una relación laboral de una persona.
type EmploymentRef struct {
	ID          string       `json:"id"`
	Person      string       `json:"person"`
	Employer    string       `json:"employer"`
	Number      string       `json:"number,omitempty"`
	Hired       string       `json:"hired"`
	Terminated  string       `json:"terminated,omitempty"`
	JobCategory string       `json:"jobCategory,omitempty"`
	WageGroup   string       `json:"wageGroup,omitempty"`
	Contract    *ContractRef `json:"contract,omitempty"` // el contrato principal en la fecha pedida
}

// Staff responde quién está empleado (lo que el Payroll del C# leía del rol Employee y de las
// tablas de LaborContract). Las personas que no están no aparecen; date es una fecha civil
// (YYYY-MM-DD).
type Staff interface {
	EmploymentsOn(ctx context.Context, personIDs []string, date string) (map[string][]EmploymentRef, error)
}

// PositionRef es un puesto con su titular actual y su puesto supervisor principal.
type PositionRef struct {
	ID           string `json:"id"`
	Unit         string `json:"unit"`
	Organization string `json:"organization"`
	Type         string `json:"type"`
	Holder       string `json:"holder,omitempty"`
	ReportsTo    string `json:"reportsTo,omitempty"`
	Active       bool   `json:"active"`
}

// Positions resuelve puestos por lotes (titulares y supervisores de ahora). Los puestos que no
// están no aparecen.
type Positions interface {
	Resolve(ctx context.Context, ids []string) (map[string]PositionRef, error)
	HeldBy(ctx context.Context, personID string) ([]PositionRef, error)
}

// EmployeeHiredV1 se publica cuando se contrata a una persona.
type EmployeeHiredV1 struct {
	EmploymentID string `json:"employmentId"`
	Person       string `json:"person"`
	Employer     string `json:"employer"`
	Number       string `json:"number,omitempty"`
	Hired        string `json:"hired"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (EmployeeHiredV1) IntegrationEventType() string { return "hr.employee-hired.v1" }

// EmployeeTerminatedV1 se publica cuando termina una relación laboral.
type EmployeeTerminatedV1 struct {
	EmploymentID string `json:"employmentId"`
	Person       string `json:"person"`
	Employer     string `json:"employer"`
	Terminated   string `json:"terminated"`
	Reason       string `json:"reason,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (EmployeeTerminatedV1) IntegrationEventType() string { return "hr.employee-terminated.v1" }

// ContractStartedV1 se publica cuando se añade un contrato de trabajo.
type ContractStartedV1 struct {
	EmploymentID string `json:"employmentId"`
	ContractID   string `json:"contractId"`
	TypeCode     string `json:"typeCode"`
	Start        string `json:"start"`
	Agreement    string `json:"agreement"`
	WorkCenter   string `json:"workCenter"`
	Primary      bool   `json:"primary"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ContractStartedV1) IntegrationEventType() string { return "hr.contract-started.v1" }

// ContractEndedV1 se publica cuando termina un contrato de trabajo.
type ContractEndedV1 struct {
	EmploymentID string `json:"employmentId"`
	ContractID   string `json:"contractId"`
	End          string `json:"end"`
	Reason       string `json:"reason,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ContractEndedV1) IntegrationEventType() string { return "hr.contract-ended.v1" }

// PositionFilledV1 se publica cuando una persona ocupa un puesto.
type PositionFilledV1 struct {
	PositionID string `json:"positionId"`
	Person     string `json:"person"`
	From       string `json:"from"` // RFC 3339
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PositionFilledV1) IntegrationEventType() string { return "hr.position-filled.v1" }

// PositionVacatedV1 se publica cuando el titular deja un puesto.
type PositionVacatedV1 struct {
	PositionID string `json:"positionId"`
	Person     string `json:"person"`
	At         string `json:"at"` // RFC 3339
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PositionVacatedV1) IntegrationEventType() string { return "hr.position-vacated.v1" }
