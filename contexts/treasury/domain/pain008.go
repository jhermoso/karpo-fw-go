package domain

import (
	"encoding/xml"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The ISO 20022 customer direct debit initiation (pain.008.001.02), the file SEPA banks accept for
// CORE and B2B collections. One payment information block per sequence type.

type painDocument struct {
	XMLName xml.Name       `xml:"Document"`
	Xmlns   string         `xml:"xmlns,attr"`
	Init    painInitiation `xml:"CstmrDrctDbtInitn"`
}

type painInitiation struct {
	Header   painHeader    `xml:"GrpHdr"`
	Payments []painPayment `xml:"PmtInf"`
}

type painHeader struct {
	MsgID     string    `xml:"MsgId"`
	Created   string    `xml:"CreDtTm"`
	Count     int       `xml:"NbOfTxs"`
	Sum       string    `xml:"CtrlSum"`
	Initiator painParty `xml:"InitgPty"`
}

type painParty struct {
	Name string   `xml:"Nm"`
	ID   *painOrg `xml:"Id,omitempty"`
}

type painOrg struct {
	Other painOther `xml:"OrgId>Othr"`
}

type painOther struct {
	ID     string `xml:"Id"`
	Scheme string `xml:"SchmeNm>Prtry,omitempty"`
}

type painAgent struct {
	BIC   string     `xml:"FinInstnId>BIC,omitempty"`
	Other *painOther `xml:"FinInstnId>Othr,omitempty"`
}

type painPayment struct {
	ID           string            `xml:"PmtInfId"`
	Method       string            `xml:"PmtMtd"`
	Batch        bool              `xml:"BtchBookg"`
	Count        int               `xml:"NbOfTxs"`
	Sum          string            `xml:"CtrlSum"`
	Service      string            `xml:"PmtTpInf>SvcLvl>Cd"`
	Instrument   string            `xml:"PmtTpInf>LclInstrm>Cd"`
	Sequence     string            `xml:"PmtTpInf>SeqTp"`
	Collection   string            `xml:"ReqdColltnDt"`
	Creditor     painParty         `xml:"Cdtr"`
	CreditorIBAN string            `xml:"CdtrAcct>Id>IBAN"`
	CreditorBank painAgent         `xml:"CdtrAgt"`
	Charges      string            `xml:"ChrgBr"`
	SchemeID     painOther         `xml:"CdtrSchmeId>Id>PrvtId>Othr"`
	Debits       []painTransaction `xml:"DrctDbtTxInf"`
}

type painTransaction struct {
	EndToEnd   string     `xml:"PmtId>EndToEndId"`
	Amount     painAmount `xml:"InstdAmt"`
	MandateID  string     `xml:"DrctDbtTx>MndtRltdInf>MndtId"`
	Signed     string     `xml:"DrctDbtTx>MndtRltdInf>DtOfSgntr"`
	DebtorBank painAgent  `xml:"DbtrAgt"`
	Debtor     painParty  `xml:"Dbtr"`
	DebtorIBAN string     `xml:"DbtrAcct>Id>IBAN"`
	Remittance string     `xml:"RmtInf>Ustrd"`
}

type painAmount struct {
	Currency string `xml:"Ccy,attr"`
	Value    string `xml:",chardata"`
}

// sepaName keeps a name within the SEPA character set and 70 characters.
func sepaName(s string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("/-?:().,'+ ", r):
			return r
		case strings.ContainsRune("áàäâ", r):
			return 'a'
		case strings.ContainsRune("ÁÀÄÂ", r):
			return 'A'
		case strings.ContainsRune("éèëê", r):
			return 'e'
		case strings.ContainsRune("ÉÈËÊ", r):
			return 'E'
		case strings.ContainsRune("íìïî", r):
			return 'i'
		case strings.ContainsRune("ÍÌÏÎ", r):
			return 'I'
		case strings.ContainsRune("óòöô", r):
			return 'o'
		case strings.ContainsRune("ÓÒÖÔ", r):
			return 'O'
		case strings.ContainsRune("úùüû", r):
			return 'u'
		case strings.ContainsRune("ÚÙÜÛ", r):
			return 'U'
		case r == 'ñ':
			return 'n'
		case r == 'Ñ':
			return 'N'
		case r == 'ç':
			return 'c'
		case r == 'Ç':
			return 'C'
		}
		return ' '
	}, strings.TrimSpace(s))
	if len(out) > 70 {
		out = out[:70]
	}
	return out
}

func agent(bic string) painAgent {
	if bic != "" {
		return painAgent{BIC: bic}
	}
	return painAgent{Other: &painOther{ID: "NOTPROVIDED"}}
}

// Pain008 renders the file of a generated remittance.
func (r *Remittance) Pain008() ([]byte, error) {
	if r.s.Status == RemDraft || r.s.Status == RemCancelled {
		return nil, fw.Violation("treasury.remittance_not_generated", "the file exists once the remittance is generated")
	}
	msg := strings.ReplaceAll(r.ID().String(), "-", "")
	doc := painDocument{Xmlns: "urn:iso:std:iso:20022:tech:xsd:pain.008.001.02", Init: painInitiation{Header: painHeader{
		MsgID: msg, Created: r.s.GeneratedAt.Format("2006-01-02T15:04:05"), Count: len(r.s.Items), Sum: r.Total().StringFixed(2),
		Initiator: painParty{Name: sepaName(r.s.CreditorName), ID: &painOrg{Other: painOther{ID: r.s.CreditorID}}}}}}
	for _, seq := range []string{"FRST", "RCUR"} {
		p := painPayment{ID: msg + "-" + seq, Method: "DD", Batch: true, Service: "SEPA", Instrument: r.s.Scheme.String(), Sequence: seq,
			Collection: r.s.CollectionDate.String(), Creditor: painParty{Name: sepaName(r.s.CreditorName)}, CreditorIBAN: r.s.CreditorIBAN.String(),
			CreditorBank: agent(r.s.CreditorBIC), Charges: "SLEV", SchemeID: painOther{ID: r.s.CreditorID, Scheme: "SEPA"}}
		sum := vocab.DecimalFromInt(0)
		for _, i := range r.s.Items {
			if i.Sequence != seq {
				continue
			}
			p.Debits = append(p.Debits, painTransaction{EndToEnd: i.EndToEnd, Amount: painAmount{Currency: "EUR", Value: i.Amount.StringFixed(2)},
				MandateID: i.MandateRef, Signed: i.Signed.String(), DebtorBank: agent(""), Debtor: painParty{Name: sepaName(i.DebtorName)},
				DebtorIBAN: i.IBAN.String(), Remittance: sepaName("Factura " + i.Number)})
			sum = sum.Add(i.Amount)
		}
		if len(p.Debits) == 0 {
			continue
		}
		p.Count, p.Sum = len(p.Debits), sum.StringFixed(2)
		doc.Init.Payments = append(doc.Init.Payments, p)
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
