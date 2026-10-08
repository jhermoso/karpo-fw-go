package domain

import (
	"strconv"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Roles of the files of Apiscore.
const (
	RolePaymentAccounts = "payment-accounts" // cuentas_pago.csv
	RoleVirtualAccounts = "virtual-accounts" // cuentas_cv.csv
	RoleOwnAccounts     = "own-accounts"     // cuentas_propias.csv
)

// Uses of an account, by the code the finance sector gives them.
const (
	UseCustomerPayment      = "customer-payment"
	UseVirtualMulticurrency = "virtual-multicurrency"
)

// apiscoreUses are the uses Apiscore names in OPERATIVA_DESCRIPCION, folded; any other is a
// payment account of a customer.
var apiscoreUses = map[string]string{"cuenta de agente": "agent", "cuenta de jugador": "player", "cuenta operativa de jugador": "player-operating",
	"cuenta transitoria de jugadores": "player-transit", "cuenta operativa de efectivo": "cash-operating"}

// Apiscore reads the files of the core banking system of a payment institution (the C#
// ApiscoreCsvReader and ApiscoreMapper), separated by commas: the accounts it keeps for its
// customers (payment accounts with an IBAN, and virtual ones) and its own accounts in banks.
//
//	payment-accounts, virtual-accounts:
//	    IBAN,FK_DIVISA,OPERATIVA_DESCRIPCION,ESTADO_DESCRIPCION,ABANDONADA,DEMO,BLOQUEADA,ID_CLIENTE,N_DOCUMENTO,
//	    TIPO_DOCUMENTO,TIPO_PERSONA,TITULAR,NOMBRE_APELLIDOS,EMPRESA,FECHA_ALTA,FECHA_BAJA[,NACIONALIDAD_CLIENTE]
//	own-accounts:
//	    ES_SEGREGADA,ACTIVO,DIVISA,IBAN,IBAN_ALL,SWIFT,BANCO
//
// The files do not say whose they are: Entity is the institution, as Parties names it.
type Apiscore struct{ Entity string }

// Key implements Source.
func (Apiscore) Key() string { return "apiscore" }

// Files implements Source.
func (Apiscore) Files() []FileSpec {
	const accounts = "IBAN,FK_DIVISA,OPERATIVA_DESCRIPCION,ESTADO_DESCRIPCION,ABANDONADA,DEMO,BLOQUEADA,ID_CLIENTE,N_DOCUMENTO,TIPO_DOCUMENTO,TIPO_PERSONA,TITULAR,NOMBRE_APELLIDOS,EMPRESA,FECHA_ALTA,FECHA_BAJA,NACIONALIDAD_CLIENTE"
	return []FileSpec{{Role: RolePaymentAccounts, About: accounts}, {Role: RoleVirtualAccounts, About: accounts},
		{Role: RoleOwnAccounts, About: "ES_SEGREGADA,ACTIVO,DIVISA,IBAN,IBAN_ALL,SWIFT,BANCO"}}
}

// Kinds implements Source.
func (Apiscore) Kinds() []string {
	return []string{KindLegalEntity, KindCustomer, KindCustomerAccount, KindOwnAccount}
}

// flag reads a flag as Apiscore writes it: true, or a number that is not zero.
func flag(s string) bool {
	if strings.EqualFold(s, "true") {
		return true
	}
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f != 0
}

// currency reads a currency as Apiscore writes it: euros when it says nothing, and the code of
// the Mexican peso it still uses.
func currency(s string) string {
	switch s = strings.ToUpper(s); s {
	case "":
		return "EUR"
	case "MXP":
		return "MXN"
	}
	return s
}

type apiscoreRun struct {
	run
	entity    string
	customers map[string]bool
	accounts  map[string]bool
	own       map[string]bool
}

// customer makes sure the holder of a row is among the records, and returns its key.
func (p *apiscoreRun) customer(t *Table, r Row) (string, bool) {
	organization := strings.HasPrefix(Fold(r.Get("TIPO_PERSONA")), "jur")
	candidates := []string{r.Get("NOMBRE_APELLIDOS"), r.Get("TITULAR"), r.Get("EMPRESA")}
	if organization {
		candidates = []string{r.Get("EMPRESA"), r.Get("TITULAR"), r.Get("NOMBRE_APELLIDOS")}
	}
	name := ""
	for _, c := range candidates {
		if name = CleanName(c); name != "" {
			break
		}
	}
	document := AccountNumber(r.Get("N_DOCUMENTO"))
	key := "doc:" + document
	if document == "" {
		key = "name:" + strings.ToUpper(name)
	}
	if name == "" {
		return "", false
	}
	if p.customers[key] {
		return key, true
	}
	p.customers[key] = true
	f := map[string]string{"code": r.Get("ID_CLIENTE"), "name": name, "partyKind": PartyPerson, "legalEntity": p.entity, "email": "", "document": ""}
	if organization {
		f["partyKind"] = PartyOrganization
	} else {
		f["fullName"] = name
	}
	if document != "" {
		if typ, country, ok := Document(document, r.Get("TIPO_DOCUMENTO"), r.Get("NACIONALIDAD_CLIENTE")); ok {
			f["document"], f["documentType"], f["country"] = document, typ, country
		} else {
			p.warn(t, r, KindCustomer, key, "apiscore.document_without_country",
				"nobody can tell which country issued the document ("+r.Get("TIPO_DOCUMENTO")+", "+r.Get("NACIONALIDAD_CLIENTE")+"): the customer is loaded without it")
		}
	}
	p.add(t, r, KindCustomer, p.entity, key, f)
	return key, true
}

func (p *apiscoreRun) customerAccounts(t *Table, virtual bool) {
	for _, r := range t.Rows {
		number := AccountNumber(r.Get("IBAN"))
		if number == "" {
			continue
		}
		if p.accounts[number] {
			p.warn(t, r, KindCustomerAccount, number, "apiscore.duplicate", "the account appears twice; the second is ignored")
			continue
		}
		opened, ok1 := Day(r.Get("FECHA_ALTA"))
		closed, ok2 := Day(r.Get("FECHA_BAJA"))
		if !ok1 || !ok2 {
			p.reject(t, r, KindCustomerAccount, number, "apiscore.dates", "the opening and closing dates cannot be read")
			continue
		}
		holder, ok := p.customer(t, r)
		if !ok {
			p.reject(t, r, KindCustomerAccount, number, "apiscore.account_without_holder", "the account has no holder: no column gives a name")
			continue
		}
		p.accounts[number] = true
		status := "active"
		switch {
		case flag(r.Get("BLOQUEADA")):
			status = "blocked"
		case flag(r.Get("ABANDONADA")):
			status = "abandoned"
		}
		use := apiscoreUses[Fold(r.Get("OPERATIVA_DESCRIPCION"))]
		switch {
		case virtual:
			use = UseVirtualMulticurrency
		case use == "":
			use = UseCustomerPayment
		}
		p.add(t, r, KindCustomerAccount, p.entity, number, map[string]string{"number": number, "virtual": strconv.FormatBool(virtual),
			"currency": currency(r.Get("FK_DIVISA")), "name": r.Get("OPERATIVA_DESCRIPCION"), "legalEntity": p.entity, "holder": holder, "use": use,
			"status": status, "demo": strconv.FormatBool(flag(r.Get("DEMO")) || Fold(r.Get("ESTADO_DESCRIPCION")) == "pruebas"),
			"opened": opened, "closed": closed})
	}
}

func (p *apiscoreRun) ownAccounts(t *Table) {
	if !t.Has("ES_SEGREGADA") {
		p.messages = append(p.messages, Message{Severity: SeverityWarning, Code: "apiscore.no_segregation_column", File: t.File, Kind: KindOwnAccount,
			Text: "the file has no column ES_SEGREGADA: every account is taken as an operating one"})
	}
	for _, r := range t.Rows {
		iban := AccountNumber(r.Get("IBAN_ALL"))
		if len(iban) < 15 { // the short column holds only the code of the bank
			iban = AccountNumber(r.Get("IBAN"))
		}
		bank := CleanName(r.Get("BANCO"))
		if iban == "" || bank == "" || p.own[iban] {
			continue
		}
		p.own[iban] = true
		p.add(t, r, KindOwnAccount, p.entity, iban, map[string]string{"iban": iban, "bic": strings.ToUpper(r.Get("SWIFT")), "bank": bank,
			"legalEntity": p.entity, "currency": currency(r.Get("DIVISA")), "ledgerAccount": "", "segregated": strconv.FormatBool(flag(r.Get("ES_SEGREGADA"))),
			"abandonment": "false"})
	}
}

// Read implements Source.
func (a Apiscore) Read(files []File) ([]Record, []Message, error) {
	entity := strings.Join(strings.Fields(a.Entity), " ")
	if entity == "" {
		return nil, nil, fw.Violation("apiscore.no_entity", "this installation does not say which institution the files of Apiscore belong to")
	}
	p := &apiscoreRun{entity: entity, customers: map[string]bool{}, accounts: map[string]bool{}, own: map[string]bool{}}
	p.records = append(p.records, Record{Kind: KindLegalEntity, Scope: GlobalScope, Key: entity,
		Fields: map[string]string{"name": entity, "financialInstitution": "true"}})
	read := 0
	for _, role := range []string{RolePaymentAccounts, RoleVirtualAccounts, RoleOwnAccounts} {
		for _, f := range files {
			if f.Role != role {
				continue
			}
			t, err := ParseTable(f, ',')
			if err != nil {
				return nil, nil, err
			}
			read++
			if role == RoleOwnAccounts {
				p.ownAccounts(t)
			} else {
				p.customerAccounts(t, role == RoleVirtualAccounts)
			}
		}
	}
	if read == 0 {
		return nil, nil, fw.Violation("apiscore.no_files", "an import of Apiscore needs at least one of its three files")
	}
	return p.records, p.messages, nil
}
