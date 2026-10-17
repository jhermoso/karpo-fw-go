// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Exchange.
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "exchange"

// ReservationRegisteredV1 se publica cuando un cliente reserva efectivo en divisa. Pickup y
// ExpiresAt son instantes RFC 3339 en UTC; Total va en euros con dos decimales.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (ReservationRegisteredV1) IntegrationEventType() string {
	return "exchange.reservation-registered.v1"
}

// ReservationStatusChangedV1 se publica en cada cambio de estado de una reserva: lo escuchan quien
// escribe al cliente (correo verificado, efectivo preparado, recogida, cancelada, caducada) y quien
// convierte un cliente potencial en cliente cuando se recoge.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (ReservationStatusChangedV1) IntegrationEventType() string {
	return "exchange.reservation-status-changed.v1"
}
