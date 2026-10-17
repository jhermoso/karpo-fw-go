package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Eventos de dominio (el RRHH del C# no lanzaba ninguno).
type (
	// PositionOpened se lanza cuando se crea un puesto.
	PositionOpened struct {
		fw.EventMeta
		Unit         string `json:"unit"`
		Organization string `json:"organization"`
		Type         string `json:"type"`
	}
	// PositionStatusChanged se lanza cuando cambia el estado.
	PositionStatusChanged struct {
		fw.EventMeta
		Status string `json:"status"`
	}
	// PositionFilled se lanza cuando una persona ocupa el puesto.
	PositionFilled struct {
		fw.EventMeta
		Holder string    `json:"holder"`
		From   time.Time `json:"from"`
	}
	// PositionVacated se lanza cuando el titular deja el puesto.
	PositionVacated struct {
		fw.EventMeta
		Holder string    `json:"holder"`
		At     time.Time `json:"at"`
	}
	// ReportingChanged se lanza cuando empieza o termina una línea de dependencia.
	ReportingChanged struct {
		fw.EventMeta
		Supervisor string `json:"supervisor"`
		Primary    bool   `json:"primary,omitempty"`
		Ended      bool   `json:"ended,omitempty"`
	}
	// PositionClosed se lanza cuando se cierra un puesto.
	PositionClosed struct {
		fw.EventMeta
		At time.Time `json:"at"`
	}
	// EmployeeHired se lanza cuando se contrata a una persona.
	EmployeeHired struct {
		fw.EventMeta
		Person   string `json:"person"`
		Employer string `json:"employer"`
		Number   string `json:"number,omitempty"`
		Hired    string `json:"hired"`
	}
	// EmployeeTerminated se lanza cuando termina una relación laboral.
	EmployeeTerminated struct {
		fw.EventMeta
		Person     string `json:"person"`
		Employer   string `json:"employer"`
		Terminated string `json:"terminated"`
		Reason     string `json:"reason,omitempty"`
	}
	// ContractStarted se lanza cuando se añade un contrato de trabajo.
	ContractStarted struct {
		fw.EventMeta
		Contract   string `json:"contract"`
		TypeCode   string `json:"typeCode"`
		Start      string `json:"start"`
		Agreement  string `json:"agreement"`
		WorkCenter string `json:"workCenter"`
		Primary    bool   `json:"primary"`
	}
	// ContractEnded se lanza cuando termina un contrato de trabajo.
	ContractEnded struct {
		fw.EventMeta
		Contract string `json:"contract"`
		End      string `json:"end"`
		Reason   string `json:"reason,omitempty"`
	}
	// WorkCenterOpened se lanza cuando se da de alta un centro de trabajo.
	WorkCenterOpened struct {
		fw.EventMeta
		Employer string `json:"employer"`
		Facility string `json:"facility"`
		Code     string `json:"code"`
	}
	// WorkCenterClosed se lanza cuando cierra un centro de trabajo.
	WorkCenterClosed struct {
		fw.EventMeta
		Closed string `json:"closed"`
	}
)

// Implementaciones de EventType.
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
