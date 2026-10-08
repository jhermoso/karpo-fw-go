// Package contracts is what other bounded contexts may depend on: the Published Language of
// Exchange.
package contracts

// Source is the name of the publishing bounded context.
const Source = "exchange"

// ReservationRegisteredV1 is published when a customer reserves foreign cash. Pickup and ExpiresAt
// are RFC 3339 instants in UTC; Total is in euros with two decimals.
type ReservationRegisteredV1 struct {
	ReservationID string `json:"reservationId"`
	Reference     string `json:"reference"`
	Company       string `json:"company"`
	Customer      string `json:"customer"`
	Channel       string `json:"channel"` // web | phone | office
	Pickup        string `json:"pickup"`
	ExpiresAt     string `json:"expiresAt"`
	Lines         int    `json:"lines"`
	Total         string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ReservationRegisteredV1) IntegrationEventType() string {
	return "exchange.reservation-registered.v1"
}

// ReservationStatusChangedV1 is published on each change of status of a reservation: whoever
// writes to the customer (email verified, cash ready, collected, cancelled, expired) and whoever
// turns a prospect into a customer when it is collected listen to it.
type ReservationStatusChangedV1 struct {
	ReservationID string `json:"reservationId"`
	Reference     string `json:"reference"`
	Company       string `json:"company"`
	Customer      string `json:"customer"`
	From          string `json:"from"`
	To            string `json:"to"` // email-verified | notified | completed | cancelled | expired
	At            string `json:"at"`
	Total         string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ReservationStatusChangedV1) IntegrationEventType() string {
	return "exchange.reservation-status-changed.v1"
}
