// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Work.
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "work"

// WorkCompletedV1 se publica cuando se termina un trabajo. Hours son las horas aprobadas anotadas
// en él y Cost lo que cuestan a las tarifas de las asignaciones; cadenas decimales con dos
// decimales.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (WorkCompletedV1) IntegrationEventType() string { return "work.work-completed.v1" }

// TimeApprovedV1 se publica cuando el tiempo que una persona trabajó en un trabajo en un día pasa a
// ser definitivo.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (TimeApprovedV1) IntegrationEventType() string { return "work.time-approved.v1" }
