package host

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	accdomain "github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	findomain "github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fisdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	impdomain "github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	treapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	tredomain "github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The loaders of what the accounting and banking sources bring (Sage, Apiscore): whom a company
// trades with, its rates of VAT, its chart of accounts, its accounts in banks and, when it is a
// financial institution, the accounts it keeps for its customers. As the others: each writes with
// the use cases of the context that owns what it loads, and leaves what exists as it is.

// documentTypes are the types of document of Parties the sources tell apart, by their code.
var documentTypes = map[string]string{impdomain.DocNational: "c0000000-0004-0000-0000-000000000002",
	impdomain.DocTax: "c0000000-0004-0000-0000-000000000003", impdomain.DocOther: "c0000000-0004-0000-0000-000000000007"}

// identify gives a party the document its record brings, when it brings one.
func identify(ctx context.Context, p *parties.Module, party string, r impdomain.Record) error {
	typ := documentTypes[r.Fields["documentType"]]
	if r.Fields["document"] == "" || typ == "" {
		return nil
	}
	pid, err := pardomain.ParsePartyID(party)
	if err != nil {
		return err
	}
	_, err = p.Service.AddIdentification.Handle(ctx, parapp.AddIdentification{PartyID: pid, DocumentType: typ, Country: r.Fields["country"],
		Number: r.Fields["document"], Primary: true})
	return err
}

// Counterparties loads the customers or the suppliers of a company into Parties: a person or an
// organization with that role, related so to the company, with its document and its email.
type Counterparties struct {
	Parties      *parties.Module
	UoW          fw.UnitOfWork
	Of           string // the kind of record: customer or supplier
	Role         pardomain.RoleTypeID
	Relationship pardomain.RelationshipTypeID
}

// Customers loads the customers of a company.
func Customers(p *parties.Module, uow fw.UnitOfWork) Counterparties {
	return Counterparties{Parties: p, UoW: uow, Of: impdomain.KindCustomer, Role: pardomain.RoleCustomer, Relationship: pardomain.RelCustomer}
}

// Suppliers loads the suppliers of a company.
func Suppliers(p *parties.Module, uow fw.UnitOfWork) Counterparties {
	return Counterparties{Parties: p, UoW: uow, Of: impdomain.KindSupplier, Role: pardomain.RoleSupplier, Relationship: pardomain.RelSupplier}
}

// Kind implements imports' Loader.
func (l Counterparties) Kind() string { return l.Of }

// EntityType implements imports' Loader.
func (Counterparties) EntityType() string { return pardomain.PartyKind }

func (l Counterparties) kind(r impdomain.Record) string {
	if r.Fields["partyKind"] == impdomain.PartyPerson {
		return string(pardomain.KindPerson)
	}
	return string(pardomain.KindOrganization)
}

// Find looks for who has that document; without one, for the only one of that name the company
// already deals with.
func (l Counterparties) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	if doc := r.Fields["document"]; doc != "" {
		found, err := l.Parties.Service.Search.Handle(ctx, parapp.SearchParties{Document: doc, Kind: l.kind(r), Size: 5})
		if err != nil || len(found.Items) > 0 {
			if err == nil && len(found.Items) == 1 {
				return found.Items[0].ID, nil
			}
			return "", err // two with the same document: nobody can tell which
		}
	}
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", nil
	}
	name := impdomain.Fold(r.Fields["name"])
	found, err := l.Parties.Service.Search.Handle(ctx, parapp.SearchParties{Text: r.Fields["name"], Kind: l.kind(r), Organization: company, Size: 100})
	if err != nil {
		return "", err
	}
	match := ""
	for _, p := range found.Items {
		if impdomain.Fold(p.Name) == name {
			if match != "" {
				return "", nil
			}
			match = p.ID
		}
	}
	return match, nil
}

// Apply registers the party with its role, its relationship with the company, its email and its
// document, all or nothing. One that exists is only related to the company, if it was not yet:
// the customer of one company of the group may be the customer of another.
func (l Counterparties) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", "", err
	}
	if existing != "" {
		pid, err := pardomain.ParsePartyID(existing)
		if err != nil {
			return "", "", err
		}
		rels, err := l.Parties.Service.Relationships.Handle(ctx, parapp.PartyRelationships{PartyID: pid, ActiveOnly: true})
		if err != nil {
			return "", "", err
		}
		for _, rel := range rels {
			if rel.Type == l.Relationship.String() && (rel.FromParty == existing && rel.ToParty == company || rel.FromParty == company && rel.ToParty == existing) {
				return existing, impdomain.Unchanged, nil
			}
		}
		err = l.UoW.Do(ctx, func(ctx context.Context) error {
			p, err := l.Parties.Service.Get.Handle(ctx, parapp.GetParty{ID: pid})
			if err != nil {
				return err
			}
			plays := false
			for _, role := range p.Roles {
				plays = plays || role.Active && role.RoleType == l.Role.String()
			}
			if !plays {
				if _, err := l.Parties.Service.AssignRole.Handle(ctx, parapp.AssignRole{PartyID: pid, RoleType: l.Role.String()}); err != nil {
					return err
				}
			}
			from, to := existing, company
			if l.Of == impdomain.KindSupplier { // the company is who has the supplier
				from, to = company, existing
			}
			_, err = l.Parties.Service.EstablishRelationship.Handle(ctx, parapp.EstablishRelationship{Type: l.Relationship.String(), From: from, To: to})
			return err
		})
		if err != nil {
			return "", "", err
		}
		return existing, impdomain.Updated, nil
	}
	id := ""
	err = l.UoW.Do(ctx, func(ctx context.Context) error {
		affiliation := &parapp.NewAffiliation{Organization: company, RelationshipType: l.Relationship.String()}
		if r.Fields["partyKind"] == impdomain.PartyPerson {
			given, surname := names(r)
			p, err := l.Parties.Service.RegisterPerson.Handle(ctx, parapp.RegisterPerson{GivenName: given, FirstSurname: surname,
				Roles: []string{l.Role.String()}, Affiliation: affiliation})
			if err != nil {
				return err
			}
			id = p.ID
		} else {
			p, err := l.Parties.Service.RegisterOrganization.Handle(ctx, parapp.RegisterOrganization{LegalName: r.Fields["name"],
				Roles: []string{l.Role.String()}, Affiliation: affiliation})
			if err != nil {
				return err
			}
			id = p.ID
		}
		if email := r.Fields["email"]; email != "" {
			pid, _ := pardomain.ParsePartyID(id)
			if _, err := l.Parties.Service.AddContact.Handle(ctx, parapp.AddContact{PartyID: pid, Kind: string(pardomain.ContactEmail), Value: email,
				Purposes: []string{"default"}}); err != nil {
				return err
			}
		}
		return identify(ctx, l.Parties, id, r)
	})
	if err != nil {
		return "", "", err
	}
	return id, impdomain.Created, nil
}

// TaxRates loads the rates of VAT of a source into the catalog of Fiscal, in the common
// territory. The catalog is one for every company, and it is Karpo's: a rate of the source that
// is already there, under its code or another, is that one; only what is missing is added.
type TaxRates struct{ Fiscal *fiscal.Module }

// Kind implements imports' Loader.
func (TaxRates) Kind() string { return impdomain.KindTaxRate }

// EntityType implements imports' Loader.
func (TaxRates) EntityType() string { return fisdomain.TaxRateKind }

func sameDecimal(a, b string) bool {
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	x, err1 := vocab.ParseDecimal(a)
	y, err2 := vocab.ParseDecimal(b)
	return err1 == nil && err2 == nil && vocab.CompareDecimal(x, y) == 0
}

// Find looks among the rates in force today for the one of that code or, failing that, the one
// of the same percentage and surcharge.
func (l TaxRates) Find(ctx context.Context, r impdomain.Record, _ impdomain.Refs) (string, error) {
	rates, err := l.Fiscal.Service.SearchRates.Handle(ctx, fisapp.SearchRates{Type: fisdomain.VAT.String(), Territory: fisdomain.Common.String(),
		On: vocab.Today(time.UTC).String()})
	if err != nil {
		return "", err
	}
	same := ""
	for _, rate := range rates {
		if rate.Code == r.Fields["code"] {
			return rate.ID, nil
		}
		if same == "" && sameDecimal(rate.Rate, r.Fields["rate"]) && sameDecimal(rate.Surcharge, r.Fields["surcharge"]) {
			same = rate.ID
		}
	}
	return same, nil
}

// Apply adds the rate to the catalog, in force from today: the source does not say since when.
func (l TaxRates) Apply(ctx context.Context, r impdomain.Record, existing string, _ impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	rate, err := l.Fiscal.Service.CreateRate.Handle(ctx, fisapp.CreateRate{Type: fisdomain.VAT.String(), Territory: fisdomain.Common.String(),
		Code: r.Fields["code"], Description: r.Fields["description"], Rate: r.Fields["rate"], Surcharge: r.Fields["surcharge"], From: vocab.Today(time.UTC)})
	if err != nil {
		return "", "", err
	}
	return rate.ID, impdomain.Created, nil
}

// ChartAccounts loads the chart of accounts of a company into Accounting.
type ChartAccounts struct{ Accounting *accounting.Module }

// Kind implements imports' Loader.
func (ChartAccounts) Kind() string { return impdomain.KindLedgerAccount }

// EntityType implements imports' Loader.
func (ChartAccounts) EntityType() string { return accdomain.AccountKind }

// Find looks for the account of that code in the chart of the company.
func (l ChartAccounts) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", nil
	}
	found, err := l.Accounting.Service.SearchAccounts.Handle(ctx, accapp.SearchAccounts{Company: company, Prefix: r.Fields["code"]})
	if err != nil {
		return "", err
	}
	for _, a := range found {
		if a.Code == r.Fields["code"] {
			return a.ID, nil
		}
	}
	return "", nil
}

// Apply adds the account to the chart. One that exists keeps the name somebody gave it.
func (l ChartAccounts) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", "", err
	}
	a, err := l.Accounting.Service.CreateAccount.Handle(ctx, accapp.CreateAccount{Company: company, Code: r.Fields["code"], Name: r.Fields["name"],
		Postable: r.Fields["postable"] == "true"})
	if err != nil {
		return "", "", err
	}
	return a.ID, impdomain.Created, nil
}

// OwnAccounts loads the accounts a company has in banks into Treasury. Treasury keeps accounts in
// euros: one in another currency is refused, saying so. What the source knows of the account and
// Treasury has no place for (the bank, whether it holds customers' money) goes in its alias.
type OwnAccounts struct{ Treasury *treasury.Module }

// Kind implements imports' Loader.
func (OwnAccounts) Kind() string { return impdomain.KindOwnAccount }

// EntityType implements imports' Loader.
func (OwnAccounts) EntityType() string { return tredomain.AccountKind }

// Find looks for the account of that IBAN among those of the company.
func (l OwnAccounts) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", nil
	}
	found, err := l.Treasury.Service.SearchAccounts.Handle(ctx, treapp.SearchAccounts{Owner: company})
	if err != nil {
		return "", err
	}
	for _, a := range found {
		if impdomain.AccountNumber(a.IBAN) == r.Fields["iban"] {
			return a.ID, nil
		}
	}
	return "", nil
}

func ownAlias(r impdomain.Record) string {
	iban := r.Fields["iban"]
	parts := []string{}
	if bank := r.Fields["bank"]; bank != "" {
		parts = append(parts, bank)
	}
	if len(iban) > 4 {
		parts = append(parts, iban[len(iban)-4:])
	}
	switch {
	case r.Fields["abandonment"] == "true":
		parts = append(parts, "(segregada, abandono)")
	case r.Fields["segregated"] == "true":
		parts = append(parts, "(segregada)")
	}
	return strings.Join(parts, " ")
}

// Apply opens the account, as of today: the sources do not say since when the company has it.
func (l OwnAccounts) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", "", err
	}
	if c := r.Fields["currency"]; c != "" && c != "EUR" {
		return "", "", fw.Violation("imports.own_account_currency", "Treasury keeps accounts in euros, and this one is in "+c)
	}
	a, err := l.Treasury.Service.OpenAccount.Handle(ctx, treapp.OpenAccount{Owner: company, IBAN: r.Fields["iban"], BIC: r.Fields["bic"],
		Alias: ownAlias(r), Opened: vocab.Today(time.UTC)})
	if err != nil {
		return "", "", err
	}
	return a.ID, impdomain.Created, nil
}

// HeldAccounts loads into the finance sector the accounts a financial institution keeps for its
// customers: opened for their holder, with their use, and blocked, set apart or closed as the
// source says they are.
type HeldAccounts struct {
	Financial *financial.Module
	UoW       fw.UnitOfWork
}

// Kind implements imports' Loader.
func (HeldAccounts) Kind() string { return impdomain.KindCustomerAccount }

// EntityType implements imports' Loader.
func (HeldAccounts) EntityType() string { return findomain.AccountKind }

// Find looks for the account of that number in the institution.
func (l HeldAccounts) Find(ctx context.Context, r impdomain.Record, refs impdomain.Refs) (string, error) {
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", nil
	}
	a, err := l.Financial.Service.Get.Handle(ctx, finapp.GetAccount{Company: company, Number: r.Fields["number"]})
	if errors.Is(err, fw.ErrNotFound) {
		return "", nil
	}
	return a.ID, err
}

// Apply opens the account and takes it to the status the source gives it, both or neither. One
// that exists is left as it is: its status is kept in Karpo from then on.
func (l HeldAccounts) Apply(ctx context.Context, r impdomain.Record, existing string, refs impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	company, err := companyOf(refs, r.Scope)
	if err != nil {
		return "", "", err
	}
	holder, ok := refs.Lookup(impdomain.KindCustomer, r.Scope, r.Fields["holder"])
	if !ok || holder == "" {
		return "", "", fw.Violation("imports.unknown_holder", "the holder of the account was not loaded: the account cannot be either")
	}
	open := finapp.OpenAccount{Company: company, Number: r.Fields["number"], Virtual: r.Fields["virtual"] == "true", Currency: r.Fields["currency"],
		Name: r.Fields["name"], Demo: r.Fields["demo"] == "true", Holder: holder, Uses: []string{r.Fields["use"]}}
	if opened, has := day(r.Fields["opened"]); has {
		open.Opened = opened
	}
	id := ""
	err = l.UoW.Do(ctx, func(ctx context.Context) error {
		a, err := l.Financial.Service.Open.Handle(ctx, open)
		if err != nil {
			return err
		}
		id = a.ID
		aid, _ := findomain.ParseAccountID(a.ID)
		closed, hasClosed := day(r.Fields["closed"])
		switch {
		case hasClosed:
			_, err = l.Financial.Service.Close.Handle(ctx, finapp.ChangeStatus{ID: aid, Reason: "import", On: closed})
		case r.Fields["status"] == "blocked":
			_, err = l.Financial.Service.Block.Handle(ctx, finapp.ChangeStatus{ID: aid, Reason: "import"})
		case r.Fields["status"] == "abandoned":
			_, err = l.Financial.Service.Abandon.Handle(ctx, finapp.ChangeStatus{ID: aid, Reason: "import"})
		}
		return err
	})
	if err != nil {
		return "", "", err
	}
	return id, impdomain.Created, nil
}

var (
	_ impdomain.Loader = Counterparties{}
	_ impdomain.Loader = TaxRates{}
	_ impdomain.Loader = ChartAccounts{}
	_ impdomain.Loader = OwnAccounts{}
	_ impdomain.Loader = HeldAccounts{}
)
