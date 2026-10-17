// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Financial y el puerto para preguntar qué cuentas tiene un cliente.
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "financial"

// AccountOpenedV1 se publica cuando una entidad abre una cuenta a un cliente. Number es el IBAN, o
// el identificador propio de la entidad cuando es Virtual; es un dato personal. Opened es un día
// (YYYY-MM-DD).
type AccountOpenedV1 struct {
	AccountID string `json:"accountId"`
	Company   string `json:"company"`
	Number    string `json:"number"`
	Currency  string `json:"currency"`
	Holder    string `json:"holder"`
	Virtual   bool   `json:"virtual"`
	Demo      bool   `json:"demo"`
	Opened    string `json:"opened"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AccountOpenedV1) IntegrationEventType() string { return "financial.account-opened.v1" }

// AccountStatusChangedV1 se publica cuando una cuenta se bloquea, se aparta como abandonada, se
// libera o se cierra. Holder es a nombre de quién estaba archivada la cuenta.
type AccountStatusChangedV1 struct {
	AccountID string `json:"accountId"`
	Company   string `json:"company"`
	Number    string `json:"number"`
	Holder    string `json:"holder"`
	From      string `json:"from"`
	To        string `json:"to"` // active | blocked | abandoned | closed
	Reason    string `json:"reason,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AccountStatusChangedV1) IntegrationEventType() string {
	return "financial.account-status-changed.v1"
}

// AgreementSignedV1 se publica cuando una entidad firma un acuerdo con un cliente. Signed, From y
// Thru son días (YYYY-MM-DD); Thru está vacío mientras no tenga fin.
type AgreementSignedV1 struct {
	AgreementID string `json:"agreementId"`
	Company     string `json:"company"`
	Customer    string `json:"customer"`
	Number      string `json:"number"`
	Family      string `json:"family"` // payment | deposit | loan | investment | leasing | other
	Product     string `json:"product,omitempty"`
	Signed      string `json:"signed"`
	From        string `json:"from"`
	Thru        string `json:"thru,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AgreementSignedV1) IntegrationEventType() string { return "financial.agreement-signed.v1" }

// AgreementTerminatedV1 se publica cuando se termina un acuerdo.
type AgreementTerminatedV1 struct {
	AgreementID string `json:"agreementId"`
	Company     string `json:"company"`
	Customer    string `json:"customer"`
	Number      string `json:"number"`
	On          string `json:"on"`
	Reason      string `json:"reason,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AgreementTerminatedV1) IntegrationEventType() string {
	return "financial.agreement-terminated.v1"
}

// AccountRef es una cuenta de un cliente, tal como la ven otros contextos.
type AccountRef struct {
	ID       string
	Number   string
	Currency string
	Status   string
	Role     string // de la party en ella: titular, autorizado o beneficiario
	Operable bool   // está activa: puede recibir y dar dinero
}

// Accounts responde con qué cuentas de una entidad está relacionada ahora una party.
type Accounts interface {
	OfParty(ctx context.Context, company, party string) ([]AccountRef, error)
}
