package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Domain events (the C# RRHH raised none).
type (
	// PositionOpened is raised when a position is created.
	PositionOpened struct {
		fw.EventMeta
		Unit         string `json:"unit"`
		Organization string `json:"organization"`
		Type         string `json:"type"`
	}
	// PositionStatusChanged is raised when the status changes.
	PositionStatusChanged struct {
		fw.EventMeta
		Status string `json:"status"`
	}
	// PositionFilled is raised when a person takes the position.
	PositionFilled struct {
		fw.EventMeta
		Holder string    `json:"holder"`
		From   time.Time `json:"from"`
	}
	// PositionVacated is raised when the holder leaves the position.
	PositionVacated struct {
		fw.EventMeta
		Holder string    `json:"holder"`
		At     time.Time `json:"at"`
	}
	// ReportingChanged is raised when a reporting line starts or ends.
	ReportingChanged struct {
		fw.EventMeta
		Supervisor string `json:"supervisor"`
		Primary    bool   `json:"primary,omitempty"`
		Ended      bool   `json:"ended,omitempty"`
	}
	// PositionClosed is raised when a position is closed.
	PositionClosed struct {
		fw.EventMeta
		At time.Time `json:"at"`
	}
	// EmployeeHired is raised when a person is hired.
	EmployeeHired struct {
		fw.EventMeta
		Person   string `json:"person"`
		Employer string `json:"employer"`
		Number   string `json:"number,omitempty"`
		Hired    string `json:"hired"`
	}
	// EmployeeTerminated is raised when an employment ends.
	EmployeeTerminated struct {
		fw.EventMeta
		Person     string `json:"person"`
		Employer   string `json:"employer"`
		Terminated string `json:"terminated"`
		Reason     string `json:"reason,omitempty"`
	}
	// ContractStarted is raised when a labor contract is added.
	ContractStarted struct {
		fw.EventMeta
		Contract   string `json:"contract"`
		TypeCode   string `json:"typeCode"`
		Start      string `json:"start"`
		Agreement  string `json:"agreement"`
		WorkCenter string `json:"workCenter"`
		Primary    bool   `json:"primary"`
	}
	// ContractEnded is raised when a labor contract ends.
	ContractEnded struct {
		fw.EventMeta
		Contract string `json:"contract"`
		End      string `json:"end"`
		Reason   string `json:"reason,omitempty"`
	}
	// WorkCenterOpened is raised when a work center is registered.
	WorkCenterOpened struct {
		fw.EventMeta
		Employer string `json:"employer"`
		Facility string `json:"facility"`
		Code     string `json:"code"`
	}
	// WorkCenterClosed is raised when a work center closes.
	WorkCenterClosed struct {
		fw.EventMeta
		Closed string `json:"closed"`
	}
)

// EventType implementations.
func (PositionOpened) EventType() string        { return "hr.position_opened" }
func (PositionStatusChanged) EventType() string { return "hr.position_status_changed" }
func (PositionFilled) EventType() string        { return "hr.position_filled" }
func (PositionVacated) EventType() string       { return "hr.position_vacated" }
func (ReportingChanged) EventType() string      { return "hr.reporting_changed" }
func (PositionClosed) EventType() string        { return "hr.position_closed" }
func (EmployeeHired) EventType() string         { return "hr.employee_hired" }
func (EmployeeTerminated) EventType() string    { return "hr.employee_terminated" }
func (ContractStarted) EventType() string       { return "hr.contract_started" }
func (ContractEnded) EventType() string         { return "hr.contract_ended" }
func (WorkCenterOpened) EventType() string      { return "hr.work_center_opened" }
func (WorkCenterClosed) EventType() string      { return "hr.work_center_closed" }
