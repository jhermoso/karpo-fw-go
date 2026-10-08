// Package contracts is what other bounded contexts may depend on: the Published Language of
// Financial and the port to ask which accounts a customer has.
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "financial"

// AccountOpenedV1 is published when an institution opens an account for a customer. Number is
// the IBAN, or the institution's own identifier when Virtual; it is personal data. Opened is a
// day (YYYY-MM-DD).
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

// IntegrationEventType implements application.IntegrationEvent.
func (AccountOpenedV1) IntegrationEventType() string { return "financial.account-opened.v1" }

// AccountStatusChangedV1 is published when an account is blocked, set apart as abandoned, released
// or closed. Holder is who the account was filed under.
type AccountStatusChangedV1 struct {
	AccountID string `json:"accountId"`
	Company   string `json:"company"`
	Number    string `json:"number"`
	Holder    string `json:"holder"`
	From      string `json:"from"`
	To        string `json:"to"` // active | blocked | abandoned | closed
	Reason    string `json:"reason,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (AccountStatusChangedV1) IntegrationEventType() string {
	return "financial.account-status-changed.v1"
}

// AccountRef is an account of a customer, as other contexts see it.
type AccountRef struct {
	ID       string
	Number   string
	Currency string
	Status   string
	Role     string // of the party on it: holder, authorized or beneficiary
	Operable bool   // it is active: it can take and give money
}

// Accounts answers which accounts of an institution a party is related to now.
type Accounts interface {
	OfParty(ctx context.Context, company, party string) ([]AccountRef, error)
}
