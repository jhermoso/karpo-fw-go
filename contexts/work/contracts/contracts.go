// Package contracts is what other bounded contexts may depend on: the Published Language of Work.
package contracts

// Source is the name of the publishing bounded context.
const Source = "work"

// WorkCompletedV1 is published when a piece of work is finished. Hours are the approved hours
// recorded against it and Cost what they cost at the rates of the assignments; decimal strings
// with two decimals.
type WorkCompletedV1 struct {
	WorkID   string `json:"workId"`
	Company  string `json:"company"`
	Code     string `json:"code"`
	Kind     string `json:"kind"` // project | task | maintenance | production
	Customer string `json:"customer,omitempty"`
	Started  string `json:"started"`
	Finished string `json:"finished"`
	Hours    string `json:"hours"`
	Cost     string `json:"cost"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (WorkCompletedV1) IntegrationEventType() string { return "work.work-completed.v1" }

// TimeApprovedV1 is published when the time a person worked on a piece of work in a day becomes
// final.
type TimeApprovedV1 struct {
	EntryID  string `json:"entryId"`
	WorkID   string `json:"workId"`
	Company  string `json:"company"`
	Person   string `json:"person"`
	Date     string `json:"date"`
	Hours    string `json:"hours"`
	Cost     string `json:"cost"`
	Billable bool   `json:"billable"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (TimeApprovedV1) IntegrationEventType() string { return "work.time-approved.v1" }
