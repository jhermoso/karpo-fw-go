package domain

import (
	"strconv"
	"strings"
)

// Roles of the files of Sage: one per concept its export writes.
const (
	RoleCompanies    = "companies"     // empresas.csv
	RoleOffices      = "offices"       // oficinas.csv
	RolePersons      = "persons"       // personas.csv
	RoleEmployees    = "employees"     // empleados.csv
	RoleCustomers    = "customers"     // clientes.csv
	RoleSuppliers    = "suppliers"     // proveedores.csv
	RoleTaxRates     = "tax-rates"     // catalogos.csv
	RoleChart        = "chart"         // plancontable.csv
	RoleBankAccounts = "bank-accounts" // cuentasbancarias.csv
)

// Sage reads the files the export of Sage writes (the C# CsvSageDataSource, SageReader and
// SageMapper), separated by semicolons, with the names of its columns in the first line:
//
//	companies:     Code;Name;BaseCurrencyCode
//	offices:       CompanyCode;Code;Name;OwnerCompanyName;Address;PostalCode;City;Province;Phone;Email
//	persons:       Dni;FirstName;LastName;Email;Gender
//	employees:     CompanyCode;CompanyName;EmployeeNumber;Dni;FullName;Email;CollectiveAgreementCode;BaseSalary;Irpf;WorkCenterCode;IsActive;HireDate;TerminationDate
//	customers:     CompanyCode;CompanyName;CustomerCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode
//	suppliers:     CompanyCode;CompanyName;SupplierCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode
//	tax-rates:     CompanyCode;CompanyName;Code;Description;Rate;SurchargeRate;IsExempt;IsNonSubject
//	chart:         CompanyCode;Code;Name
//	bank-accounts: CompanyCode;BankCode;Iban;Bic;AccountHolderName;LedgerAccountCode
type Sage struct{}

// Key implements Source.
func (Sage) Key() string { return "sage" }

// Files implements Source.
func (Sage) Files() []FileSpec {
	return []FileSpec{
		{Role: RoleCompanies, Required: true, About: "Code;Name;BaseCurrencyCode"},
		{Role: RoleOffices, About: "CompanyCode;Code;Name;OwnerCompanyName;Address;PostalCode;City;Province;Phone;Email"},
		{Role: RolePersons, About: "Dni;FirstName;LastName;Email;Gender"},
		{Role: RoleEmployees, About: "CompanyCode;CompanyName;EmployeeNumber;Dni;FullName;Email;CollectiveAgreementCode;BaseSalary;Irpf;WorkCenterCode;IsActive;HireDate;TerminationDate"},
		{Role: RoleCustomers, About: "CompanyCode;CompanyName;CustomerCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode"},
		{Role: RoleSuppliers, About: "CompanyCode;CompanyName;SupplierCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode"},
		{Role: RoleTaxRates, About: "CompanyCode;CompanyName;Code;Description;Rate;SurchargeRate;IsExempt;IsNonSubject"},
		{Role: RoleChart, About: "CompanyCode;Code;Name"},
		{Role: RoleBankAccounts, About: "CompanyCode;BankCode;Iban;Bic;AccountHolderName;LedgerAccountCode"},
	}
}

// Kinds implements Source.
func (Sage) Kinds() []string {
	return []string{KindLegalEntity, KindWorkCenter, KindPerson, KindEmployment, KindWorkPlace, KindCustomer, KindSupplier, KindTaxRate,
		KindLedgerAccount, KindOwnAccount}
}

// bankLedger says what the ledger account a bank account of Sage is posted to tells of it: its
// currency and whether it holds what customers abandoned. Every account of the table is segregated
// (customers' money); one that is not in it is an operating account in euros. (The C#
// BancosContaUsePolicy: the use is told by the ledger account, never by the name of the bank.)
var bankLedger = map[string]struct {
	currency    string
	abandonment bool
}{
	"57200011": {"EUR", true}, "57200016": {"EUR", false}, "57200052": {"EUR", false}, "57200054": {"EUR", false}, "57200060": {"EUR", false},
	"57200076": {"EUR", false}, "57200100": {"EUR", false}, "57300001": {"USD", false}, "57300013": {"USD", false}, "57300017": {"USD", false}, "57300018": {"GBP", false},
	"57300019": {"JPY", false}, "57300020": {"CHF", false}, "57300021": {"NOK", false}, "57300022": {"DKK", false}, "57300023": {"SEK", false},
	"57300025": {"CAD", false}, "57300026": {"AUD", false}, "57300027": {"MXN", false}, "57300028": {"CNH", false}, "57300029": {"PLN", false},
	"57300053": {"CHF", false}, "57300076": {"USD", false}, "57300088": {"GBP", false},
}

type sageRun struct {
	run
	byCode  map[string]string            // company code → its name
	byName  map[string]string            // folded name → the name as declared
	offices map[string]string            // company|office code, folded → the name of the office
	persons map[string]map[string]string // DNI → who it is
}

// company returns the company of a row: the one of its code, or the one of the name it gives.
func (p *sageRun) company(r Row, names ...string) (string, bool) {
	if n, ok := p.byCode[strings.TrimLeft(r.Get("CompanyCode"), "0")]; ok {
		return n, true
	}
	n, ok := p.byName[Fold(r.Get(names...))]
	return n, ok
}

func (p *sageRun) companies(t *Table) {
	for _, r := range t.Rows {
		name := strings.Join(strings.Fields(r.Get("Name")), " ")
		code := strings.TrimLeft(r.Get("Code"), "0")
		switch {
		case name == "":
			p.reject(t, r, KindLegalEntity, code, "sage.company_without_name", "the company has no name")
		case p.byName[Fold(name)] != "" || p.byCode[code] != "":
			p.warn(t, r, KindLegalEntity, name, "sage.duplicate", "the company is declared twice; the second is ignored")
		default:
			p.byName[Fold(name)] = name
			if code != "" {
				p.byCode[code] = name
			}
			p.add(t, r, KindLegalEntity, GlobalScope, name, map[string]string{"name": name, "code": r.Get("Code"), "currency": strings.ToUpper(r.Get("BaseCurrencyCode"))})
		}
	}
}

func (p *sageRun) officesOf(t *Table) {
	seen := map[string]bool{}
	for _, r := range t.Rows {
		name := strings.Join(strings.Fields(r.Get("Name")), " ")
		if name == "" {
			p.reject(t, r, KindWorkCenter, r.Get("Code"), "sage.office_without_name", "the office has no name")
			continue
		}
		company, ok := p.company(r, "OwnerCompanyName")
		if !ok {
			p.reject(t, r, KindWorkCenter, name, "sage.unknown_company", "the office belongs to a company the file of companies does not declare")
			continue
		}
		if code := r.Get("Code"); code != "" {
			if _, dup := p.offices[Fold(company)+"|"+Fold(code)]; !dup {
				p.offices[Fold(company)+"|"+Fold(code)] = name
			}
		}
		if seen[Fold(company)+"|"+Fold(name)] {
			continue // the same office under another code: one work center
		}
		seen[Fold(company)+"|"+Fold(name)] = true
		where := []string{}
		for _, part := range []string{r.Get("Address"), strings.TrimSpace(r.Get("PostalCode") + " " + r.Get("City")), r.Get("Province")} {
			if part != "" {
				where = append(where, part)
			}
		}
		p.add(t, r, KindWorkCenter, company, name, map[string]string{"name": name, "legalEntity": company, "notes": strings.Join(where, ", ")})
	}
}

func (p *sageRun) personsOf(t *Table) {
	for _, r := range t.Rows {
		dni := AccountNumber(r.Get("Dni"))
		if _, dup := p.persons[dni]; dni == "" || dup {
			continue // the first of a document is who it is
		}
		p.persons[dni] = map[string]string{"firstName": r.Get("FirstName"), "lastName": r.Get("LastName"), "email": strings.ToLower(r.Get("Email")),
			"gender": r.Get("Gender")}
	}
}

func (p *sageRun) employees(t *Table) {
	seen := map[string]bool{}
	for _, r := range t.Rows {
		number := r.Get("EmployeeNumber")
		if number == "" {
			p.reject(t, r, KindPerson, "", "sage.employee_without_number", "the employee has no number")
			continue
		}
		if seen[Fold(number)] {
			p.warn(t, r, KindPerson, number, "sage.duplicate", "the employee number appears twice; the second is ignored")
			continue
		}
		company, ok := p.company(r, "CompanyName")
		if !ok {
			p.reject(t, r, KindPerson, number, "sage.unknown_company", "the employee works for a company the file of companies does not declare")
			continue
		}
		dni := AccountNumber(r.Get("Dni"))
		who := map[string]string{"firstName": "", "lastName": "", "email": strings.ToLower(r.Get("Email")), "gender": ""}
		if known, found := p.persons[dni]; found {
			who = map[string]string{"firstName": known["firstName"], "lastName": known["lastName"], "email": known["email"], "gender": known["gender"]}
			if who["email"] == "" {
				who["email"] = strings.ToLower(r.Get("Email"))
			}
		}
		full := strings.TrimSpace(who["firstName"] + " " + who["lastName"])
		if who["firstName"] == "" || who["lastName"] == "" {
			full = strings.Join(strings.Fields(r.Get("FullName")), " ")
		}
		if full == "" {
			p.reject(t, r, KindPerson, number, "sage.employee_without_name", "nobody knows the name of the employee: neither the file of persons nor this one gives it")
			continue
		}
		hired, okHired := Day(r.Get("HireDate"))
		left, okLeft := Day(r.Get("TerminationDate"))
		if !okHired || !okLeft || (hired != "" && left != "" && left < hired) {
			p.reject(t, r, KindPerson, number, "sage.dates", "the hire and termination dates cannot be read, or the employee left before arriving")
			continue
		}
		if active := r.Get("IsActive"); t.Has("IsActive") && active != "true" && active != "1" && left == "" && hired != "" {
			left = hired // Sage says they left and not when: the day they arrived is the only one known
			p.warn(t, r, KindEmployment, number, "sage.left_without_date", "the employee is not active and has no termination date: the employment ends the day it began")
		}
		seen[Fold(number)] = true
		who["employeeNumber"], who["fullName"], who["legalEntity"], who["document"] = number, full, company, ""
		if typ, country, found := Document(dni, "dni", ""); found {
			who["document"], who["documentType"], who["country"] = dni, typ, country
		}
		p.add(t, r, KindPerson, GlobalScope, number, who)
		p.add(t, r, KindEmployment, company, number, map[string]string{"employeeNumber": number, "legalEntity": company, "primary": "true",
			"hireDate": hired, "terminationDate": left})
		if code := r.Get("WorkCenterCode"); code != "" {
			if office, found := p.offices[Fold(company)+"|"+Fold(code)]; found {
				p.add(t, r, KindWorkPlace, company, number, map[string]string{"employeeNumber": number, "legalEntity": company, "workCenter": office, "from": hired})
			} else {
				p.warn(t, r, KindWorkPlace, number, "sage.unknown_office", "the employee works at an office the file of offices does not declare: "+code)
			}
		}
	}
}

// trade reads the customers or the suppliers of each company.
func (p *sageRun) trade(t *Table, kind, column string) {
	seen := map[string]bool{}
	for _, r := range t.Rows {
		code, name := r.Get(column), strings.Join(strings.Fields(r.Get("Name")), " ")
		if code == "" || name == "" {
			p.reject(t, r, kind, code, "sage.without_code_or_name", "the row has no code or no name")
			continue
		}
		company, ok := p.company(r, "CompanyName")
		if !ok {
			p.reject(t, r, kind, code, "sage.unknown_company", "the row belongs to a company the file of companies does not declare")
			continue
		}
		if seen[Fold(company)+"|"+Fold(code)] {
			p.warn(t, r, kind, code, "sage.duplicate", "the code appears twice in the company; the second is ignored")
			continue
		}
		seen[Fold(company)+"|"+Fold(code)] = true
		f := map[string]string{"code": code, "name": name, "partyKind": PartyOrganization, "legalEntity": company, "email": strings.ToLower(r.Get("Email")),
			"document": "", "paymentTerms": r.Get("PaymentConditionCode"), "agent": r.Get("CommissionistCode")}
		if tax := AccountNumber(r.Get("TaxId")); tax != "" {
			if typ, country, found := Document(tax, "cif", ""); found {
				f["document"], f["documentType"], f["country"] = tax, typ, country
			}
		}
		p.add(t, r, kind, company, code, f)
	}
}

// percent reads a percentage as Sage writes it, with a dot.
func percent(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f > 100 {
		return "", false
	}
	return strconv.FormatFloat(f, 'f', -1, 64), true
}

// taxRates reads the rates of VAT. Sage keeps one catalog and its export repeats it for every
// company: here each code is one rate.
func (p *sageRun) taxRates(t *Table) {
	seen := map[string]bool{}
	for _, r := range t.Rows {
		code := strings.ToUpper(r.Get("Code"))
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		if r.Get("IsExempt") == "true" || r.Get("IsExempt") == "1" || r.Get("IsNonSubject") == "true" || r.Get("IsNonSubject") == "1" {
			p.warn(t, r, KindTaxRate, code, "sage.exemption_is_not_a_rate", "an exemption is a tax treatment, not a rate: it is not loaded")
			continue
		}
		rate, ok1 := percent(r.Get("Rate"))
		surcharge, ok2 := percent(r.Get("SurchargeRate"))
		if !ok1 || !ok2 || rate == "" {
			p.reject(t, r, KindTaxRate, code, "sage.rate", "the rate or its surcharge is not a percentage")
			continue
		}
		description := r.Get("Description")
		if description == "" {
			description = code
		}
		p.add(t, r, KindTaxRate, GlobalScope, code, map[string]string{"code": code, "description": description, "rate": rate, "surcharge": surcharge})
	}
}

// chart reads the chart of accounts of each company. Sage does not say which accounts are posted
// to: those no other account of the company hangs from.
func (p *sageRun) chart(t *Table) {
	type account struct {
		row           Row
		company, code string
	}
	accounts, codes := []account{}, map[string][]string{}
	seen := map[string]bool{}
	for _, r := range t.Rows {
		code, name := r.Get("Code"), r.Get("Name")
		if code == "" || name == "" {
			p.reject(t, r, KindLedgerAccount, code, "sage.without_code_or_name", "the account has no code or no name")
			continue
		}
		company, ok := p.company(r)
		if !ok {
			p.reject(t, r, KindLedgerAccount, code, "sage.unknown_company", "the account belongs to a company the file of companies does not declare")
			continue
		}
		if seen[company+"|"+code] {
			continue
		}
		seen[company+"|"+code] = true
		accounts = append(accounts, account{r, company, code})
		codes[company] = append(codes[company], code)
	}
	for _, a := range accounts {
		postable := true
		for _, other := range codes[a.company] {
			postable = postable && (other == a.code || !strings.HasPrefix(other, a.code))
		}
		p.add(t, a.row, KindLedgerAccount, a.company, a.code, map[string]string{"code": a.code, "name": a.row.Get("Name"), "legalEntity": a.company,
			"postable": strconv.FormatBool(postable)})
	}
}

func (p *sageRun) bankAccounts(t *Table) {
	seen := map[string]bool{}
	for _, r := range t.Rows {
		iban := AccountNumber(r.Get("Iban"))
		if iban == "" || seen[iban] {
			continue
		}
		company, ok := p.company(r)
		if !ok {
			p.reject(t, r, KindOwnAccount, iban, "sage.unknown_company", "the bank account belongs to a company the file of companies does not declare")
			continue
		}
		seen[iban] = true
		ledger := r.Get("LedgerAccountCode")
		policy, segregated := bankLedger[ledger]
		if !segregated {
			policy.currency = "EUR"
		}
		p.add(t, r, KindOwnAccount, company, iban, map[string]string{"iban": iban, "bic": strings.ToUpper(r.Get("Bic")), "bank": r.Get("BankCode"),
			"legalEntity": company, "currency": policy.currency, "ledgerAccount": ledger, "segregated": strconv.FormatBool(segregated),
			"abandonment": strconv.FormatBool(policy.abandonment)})
	}
}

// Read implements Source.
func (Sage) Read(files []File) ([]Record, []Message, error) {
	p := &sageRun{byCode: map[string]string{}, byName: map[string]string{}, offices: map[string]string{}, persons: map[string]map[string]string{}}
	// In this order: every file names the companies, the employees name the offices and the persons.
	for _, role := range []string{RoleCompanies, RoleOffices, RolePersons, RoleEmployees, RoleCustomers, RoleSuppliers, RoleTaxRates, RoleChart, RoleBankAccounts} {
		for _, f := range files {
			if f.Role != role {
				continue
			}
			t, err := ParseTable(f, ';')
			if err != nil {
				return nil, nil, err
			}
			switch role {
			case RoleCompanies:
				p.companies(t)
			case RoleOffices:
				p.officesOf(t)
			case RolePersons:
				p.personsOf(t)
			case RoleEmployees:
				p.employees(t)
			case RoleCustomers:
				p.trade(t, KindCustomer, "CustomerCode")
			case RoleSuppliers:
				p.trade(t, KindSupplier, "SupplierCode")
			case RoleTaxRates:
				p.taxRates(t)
			case RoleChart:
				p.chart(t)
			case RoleBankAccounts:
				p.bankAccounts(t)
			}
		}
	}
	return p.records, p.messages, nil
}
