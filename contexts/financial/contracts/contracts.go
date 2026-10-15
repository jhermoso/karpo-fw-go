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

// AgreementSignedV1 is published when an institution signs an agreement with a customer. Signed,
// From and Thru are days (YYYY-MM-DD); Thru is empty while it has no end.
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

// IntegrationEventType implements application.IntegrationEvent.
func (AgreementSignedV1) IntegrationEventType() string { return "financial.agreement-signed.v1" }

// AgreementTerminatedV1 is published when an agreement is terminated.
type AgreementTerminatedV1 struct {
	AgreementID string `json:"agreementId"`
	Company     string `json:"company"`
	Customer    string `json:"customer"`
	Number      string `json:"number"`
	On          string `json:"on"`
	Reason      string `json:"reason,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (AgreementTerminatedV1) IntegrationEventType() string {
	return "financial.agreement-terminated.v1"
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
