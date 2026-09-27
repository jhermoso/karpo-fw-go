package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Domain events (the C# raised only a generic FactIssued when Documents numbered a payslip).
type (
	// PayslipDrafted is raised when a payslip is drafted.
	PayslipDrafted struct {
		fw.EventMeta
		Employment string `json:"employment"`
		Employer   string `json:"employer"`
		Kind       string `json:"kind"`
		Start      string `json:"start"`
		End        string `json:"end"`
	}
	// PayslipApproved is raised when a payslip is approved, with its totals.
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
	// PayslipCancelled is raised when an approved payslip is cancelled.
	PayslipCancelled struct {
		fw.EventMeta
		Reason string `json:"reason"`
	}
	// ProfileChanged is raised when the payroll terms of an employment change.
	ProfileChanged struct {
		fw.EventMeta
		Employment string `json:"employment"`
	}
	// EmployerAccountRegistered is raised when a CCC is registered.
	EmployerAccountRegistered struct {
		fw.EventMeta
		Employer string `json:"employer"`
		Code     string `json:"code"`
	}
	// EmployerAccountDeactivated is raised on the Social Security "baja" of a CCC.
	EmployerAccountDeactivated struct {
		fw.EventMeta
	}
)

// EventType implementations.
func (PayslipDrafted) EventType() string             { return "payroll.payslip_drafted" }
func (PayslipApproved) EventType() string            { return "payroll.payslip_approved" }
func (PayslipCancelled) EventType() string           { return "payroll.payslip_cancelled" }
func (ProfileChanged) EventType() string             { return "payroll.profile_changed" }
func (EmployerAccountRegistered) EventType() string  { return "payroll.employer_account_registered" }
func (EmployerAccountDeactivated) EventType() string { return "payroll.employer_account_deactivated" }
