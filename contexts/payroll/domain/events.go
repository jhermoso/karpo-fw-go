package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Eventos de dominio (el C# sólo lanzaba un FactIssued genérico cuando Documents numeraba una
// nómina).
type (
	// PayslipDrafted se lanza al preparar una nómina en borrador.
	PayslipDrafted struct {
		fw.EventMeta
		Employment string `json:"employment"`
		Employer   string `json:"employer"`
		Kind       string `json:"kind"`
		Start      string `json:"start"`
		End        string `json:"end"`
	}
	// PayslipApproved se lanza al aprobar una nómina, con sus totales.
	PayslipApproved struct {
		fw.EventMeta
		Employment      string `json:"employment"`
		Person          string `json:"person"`
		Employer        string `json:"employer"`
		Kind            string `json:"kind"`
		Start           string `json:"start"`
		End             string `json:"end"`
		PaymentDate     string `json:"paymentDate"`
		EmployerAccount string `json:"employerAccount,omitempty"`
		PerceptionKey   string `json:"perceptionKey,omitempty"`
		Totals          Totals `json:"totals"`
	}
	// PayslipCancelled se lanza al anular una nómina aprobada.
	PayslipCancelled struct {
		fw.EventMeta
		Reason string `json:"reason"`
	}
	// ProfileChanged se lanza cuando cambian las condiciones de nómina de una relación laboral.
	ProfileChanged struct {
		fw.EventMeta
		Employment string `json:"employment"`
	}
	// EmployerAccountRegistered se lanza al dar de alta un CCC.
	EmployerAccountRegistered struct {
		fw.EventMeta
		Employer string `json:"employer"`
		Code     string `json:"code"`
	}
	// EmployerAccountDeactivated se lanza con la baja de un CCC en la Seguridad Social.
	EmployerAccountDeactivated struct {
		fw.EventMeta
	}
)

// Implementaciones de EventType.
func (PayslipDrafted) EventType() string             { return "payroll.payslip_drafted" }
func (PayslipApproved) EventType() string            { return "payroll.payslip_approved" }
func (PayslipCancelled) EventType() string           { return "payroll.payslip_cancelled" }
func (ProfileChanged) EventType() string             { return "payroll.profile_changed" }
func (EmployerAccountRegistered) EventType() string  { return "payroll.employer_account_registered" }
func (EmployerAccountDeactivated) EventType() string { return "payroll.employer_account_deactivated" }
