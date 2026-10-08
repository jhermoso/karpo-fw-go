// Package contracts is what other bounded contexts may depend on: the Published Language of
// Imports.
package contracts

// Source is the name of the publishing bounded context.
const Source = "imports"

// RunFinishedV1 is published when an import run ends, however it does. StartedAt and FinishedAt
// are RFC 3339 instants in UTC.
type RunFinishedV1 struct {
	RunID      string `json:"runId"`
	Source     string `json:"source"` // personio...
	Status     string `json:"status"` // succeeded | completed-with-errors | failed
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	Errors     int    `json:"errors"`
	Warnings   int    `json:"warnings"`
	Created    int    `json:"created"`
	Updated    int    `json:"updated"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (RunFinishedV1) IntegrationEventType() string { return "imports.run-finished.v1" }
