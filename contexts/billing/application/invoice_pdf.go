package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/render/pdf"
)

// InvoicePDF pide el PDF de una factura emitida.
type InvoicePDF struct{ ID domain.InvoiceID }

// PDFDocument es un documento PDF con el nombre de fichero que le corresponde.
type PDFDocument struct {
	FileName string
	Content  []byte
}

// Textos del PDF de la factura, en un solo sitio para poder traducirlos después.
const (
	txtInvoice          = "FACTURA"
	txtCorrective       = "FACTURA RECTIFICATIVA"
	txtNumber           = "Número"
	txtIssueDate        = "Fecha de expedición"
	txtOperationDate    = "Fecha de la operación"
	txtDueDate          = "Vencimiento"
	txtSeller           = "EMISOR"
	txtCustomer         = "DESTINATARIO"
	txtNIF              = "NIF"
	txtDescription      = "Descripción de la operación"
	txtCorrects         = "Rectifica la factura %s, de %s."
	txtReason           = "Causa %s: %s."
	txtConcept          = "Concepto"
	txtQuantity         = "Cantidad"
	txtPrice            = "Precio"
	txtDiscount         = "Dto. %"
	txtAmount           = "Importe"
	txtTaxes            = "Impuestos"
	txtTax              = "Impuesto"
	txtBase             = "Base imponible"
	txtRate             = "Tipo %"
	txtQuota            = "Cuota"
	txtSurchargeRate    = "Rec. %"
	txtSurchargeQuota   = "Cuota rec."
	txtExempt           = "Exenta (%s)"
	txtNotSubject       = "No sujeta (%s)"
	txtTotalBase        = "Base imponible"
	txtTotalTax         = "Cuota"
	txtTotalSurcharge   = "Recargo de equivalencia"
	txtTotal            = "TOTAL"
	txtExemptMention    = "Operación exenta del impuesto, causa %s"
	txtNotSubjMention   = "Operación no sujeta al impuesto, causa %s"
	txtContinued        = "(continúa)"
	txtPage             = "Página %d de %d"
	txtVAT              = "IVA"
	txtIGIC             = "IGIC"
	txtIPSI             = "IPSI"
	txtEuro             = "€"
	txtNotIssuedMessage = "only an issued invoice has a PDF"
)

// Causas de exención y de no sujeción del IVA, con las claves del SII.
var exemptionCauses = map[string]string{
	"E1": "artículo 20 de la Ley 37/1992 del IVA",
	"E2": "artículo 21 de la Ley 37/1992 del IVA",
	"E3": "artículo 22 de la Ley 37/1992 del IVA",
	"E4": "artículos 23 y 24 de la Ley 37/1992 del IVA",
	"E5": "artículo 25 de la Ley 37/1992 del IVA",
	"E6": "otras causas de exención",
	"N1": "artículos 7, 14 u otros de la Ley 37/1992 del IVA",
	"N2": "reglas de localización",
}

// Causas de rectificación R1–R5.
var correctionReasonTexts = map[string]string{
	"R1": "error fundado en derecho y artículo 80 Uno, Dos y Seis de la Ley 37/1992 del IVA",
	"R2": "artículo 80 Tres de la Ley 37/1992 del IVA (concurso)",
	"R3": "artículo 80 Cuatro de la Ley 37/1992 del IVA (créditos incobrables)",
	"R4": "resto de causas",
	"R5": "rectificación de una factura simplificada",
}

var taxNames = map[string]string{"vat": txtVAT, "igic": txtIGIC, "ipsi": txtIPSI}

func (s service) invoicePDFUseCase(svc *Service) {
	svc.InvoicePDF = guard(PermInvoiceRead, func(ctx context.Context, q InvoicePDF) (PDFDocument, error) {
		i, err := s.Invoices.Get(ctx, q.ID)
		if err != nil {
			return PDFDocument{}, err
		}
		st := i.State()
		if err := scopeOf(ctx).check(domain.InvoiceKind, i.ID(), st.Seller, false); err != nil {
			return PDFDocument{}, err
		}
		if st.Status != domain.Issued {
			return PDFDocument{}, fw.Violation("billing.invoice_not_issued", txtNotIssuedMessage)
		}
		var original *domain.InvoiceState
		if st.Kind == domain.Corrective {
			o, err := s.Invoices.Get(ctx, st.Corrects)
			if err != nil {
				return PDFDocument{}, err
			}
			os := o.State()
			original = &os
		}
		name := strings.NewReplacer("/", "-", "\\", "-", "\"", "").Replace(st.Number) + ".pdf"
		return PDFDocument{FileName: name, Content: renderInvoice(st, original)}, nil
	})
}

// Maquetación, en puntos sobre A4 (595 × 842), midiendo desde arriba.
const (
	left, right   = 40.0, 555.0
	bottom        = 790.0 // por debajo, sólo el pie de página
	lineHeight    = 12.0
	bodySize      = 9.0
	smallSize     = 8.0
	columnWidth   = 245.0
	customerX     = 310.0
	conceptWidth  = 195.0 // el concepto acaba en 235
	quantityRight = 310.0 // cabe 1.234,5678
	priceRight    = 400.0 // cabe 1.234.567,123456 (unos 73 puntos)
	discountRight = 450.0
	qrSize        = 85.0 // unos 30 mm
)

// sheet es la página en curso y la altura por la que va la escritura.
type sheet struct {
	doc  *pdf.Document
	page *pdf.Page
	y    float64
}

func (s *sheet) newPage() {
	s.page, s.y = s.doc.AddPage(), 50
}

// need pasa a una página nueva si no caben h puntos más; dice si lo ha hecho.
func (s *sheet) need(h float64) bool {
	if s.y+h <= bottom {
		return false
	}
	s.page.Text(left, bottom+15, pdf.Regular, smallSize, txtContinued)
	s.newPage()
	return true
}

func (s *sheet) text(x float64, f pdf.Font, size float64, t string) { s.page.Text(x, s.y, f, size, t) }

func (s *sheet) right(x float64, f pdf.Font, size float64, t string) {
	s.page.TextRight(x, s.y, f, size, t)
}

// paragraph escribe un texto partido en líneas a partir de x.
func (s *sheet) paragraph(x, width float64, f pdf.Font, t string) {
	for _, l := range pdf.Wrap(f, bodySize, width, t) {
		s.need(lineHeight)
		s.text(x, f, bodySize, l)
		s.y += lineHeight
	}
}

// renderInvoice maqueta una factura emitida con el contenido del artículo 6 del Reglamento de
// facturación (RD 1619/2012) que guarda la factura. original es la factura rectificada, si la hay.
func renderInvoice(st domain.InvoiceState, original *domain.InvoiceState) []byte {
	s := &sheet{doc: pdf.New()}
	s.newPage()

	// Hueco reservado para el código QR de VERI*FACTU (plan Sage, T9): arriba a la derecha, unos
	// 30 × 30 mm, como pide la orden HAC/1177/2024. Hoy queda vacío.
	s.page.Rect(right-qrSize, 40, qrSize, qrSize, 0.5)

	title := txtInvoice
	if st.Kind == domain.Corrective {
		title = txtCorrective
	}
	s.text(left, pdf.Bold, 18, title)
	s.y += 24
	header := [][2]string{{txtNumber, st.Number}, {txtIssueDate, esDate(st.IssueDate)}}
	if !st.OperationDate.IsZero() && st.OperationDate != st.IssueDate {
		header = append(header, [2]string{txtOperationDate, esDate(st.OperationDate)})
	}
	if !st.DueDate.IsZero() {
		header = append(header, [2]string{txtDueDate, esDate(st.DueDate)})
	}
	for _, h := range header {
		s.text(left, pdf.Bold, bodySize, h[0]+":")
		s.text(left+110, pdf.Regular, bodySize, h[1])
		s.y += lineHeight
	}
	s.y = max(s.y, 40+qrSize) + 20

	// Las partes, en dos columnas y en la misma página.
	seller, customer := partyLines(txtSeller, st.SellerIdentity), partyLines(txtCustomer, st.CustomerIdentity)
	s.need(float64(max(len(seller), len(customer))) * lineHeight)
	top := s.y
	for k, l := range seller {
		s.page.Text(left, top+float64(k)*lineHeight, l.font, l.size, l.text)
	}
	for k, l := range customer {
		s.page.Text(customerX, top+float64(k)*lineHeight, l.font, l.size, l.text)
	}
	s.y = top + float64(max(len(seller), len(customer)))*lineHeight + 10

	s.text(left, pdf.Bold, bodySize, txtDescription+":")
	s.y += lineHeight
	s.paragraph(left, right-left, pdf.Regular, st.Description)
	if original != nil {
		s.y += 4
		s.paragraph(left, right-left, pdf.Regular, fmt.Sprintf(txtCorrects, original.Number, esDate(original.IssueDate)))
		s.paragraph(left, right-left, pdf.Regular, fmt.Sprintf(txtReason, st.Reason, correctionReasonTexts[st.Reason]))
	}
	s.y += 10

	// Las líneas.
	lineHeader := func() {
		s.text(left, pdf.Bold, bodySize, txtConcept)
		s.right(quantityRight, pdf.Bold, bodySize, txtQuantity)
		s.right(priceRight, pdf.Bold, bodySize, txtPrice)
		s.right(discountRight, pdf.Bold, bodySize, txtDiscount)
		s.right(right, pdf.Bold, bodySize, txtAmount)
		s.page.Line(left, s.y+4, right, s.y+4, 0.5)
		s.y += lineHeight + 4
	}
	s.need(3 * lineHeight)
	lineHeader()
	for _, l := range st.Lines {
		concept := pdf.Wrap(pdf.Regular, bodySize, conceptWidth, l.Description)
		if s.need(float64(len(concept)) * lineHeight) {
			lineHeader()
		}
		s.right(quantityRight, pdf.Regular, bodySize, esDecimal(l.Quantity, 0))
		s.right(priceRight, pdf.Regular, bodySize, esDecimal(l.UnitPrice, 2))
		if !l.Discount.IsZero() {
			s.right(discountRight, pdf.Regular, bodySize, esDecimal(l.Discount, 0))
		}
		s.right(right, pdf.Regular, bodySize, euros(l.Net))
		for _, c := range concept {
			s.text(left, pdf.Regular, bodySize, c)
			s.y += lineHeight
		}
	}
	s.page.Line(left, s.y-8, right, s.y-8, 0.5)
	s.y += 10

	// El desglose: base, tipo y cuota por tipo, y la mención de las exentas y no sujetas.
	surcharge := !st.Taxes.Surcharge.IsZero()
	cols := []float64{250, 320, 400, 470, right} // base, tipo, cuota, rec. %, cuota rec. (por la derecha)
	taxHeader := func() {
		s.text(left, pdf.Bold, bodySize, txtTax)
		for k, h := range []string{txtBase, txtRate, txtQuota, txtSurchargeRate, txtSurchargeQuota} {
			if k < 3 || surcharge {
				s.right(cols[k], pdf.Bold, bodySize, h)
			}
		}
		s.page.Line(left, s.y+4, right, s.y+4, 0.5)
		s.y += lineHeight + 4
	}
	s.need(4 * lineHeight)
	s.text(left, pdf.Bold, bodySize, txtTaxes)
	s.y += lineHeight
	taxHeader()
	igic := false
	type mention struct{ format, code string }
	var mentions []mention
	for _, t := range st.Taxes.Lines {
		if s.need(lineHeight) {
			taxHeader()
		}
		igic = igic || t.TaxType == "igic"
		name := taxNames[t.TaxType]
		if name == "" {
			name = strings.ToUpper(t.TaxType)
		}
		switch t.TreatmentKind {
		case "exempt":
			name = fmt.Sprintf(txtExempt, t.Treatment)
			mentions = append(mentions, mention{txtExemptMention, t.Treatment})
		case "not-subject":
			name = fmt.Sprintf(txtNotSubject, t.Treatment)
			mentions = append(mentions, mention{txtNotSubjMention, t.Treatment})
		}
		s.text(left, pdf.Regular, bodySize, name)
		s.right(cols[0], pdf.Regular, bodySize, euros(t.Base))
		if t.TreatmentKind != "exempt" && t.TreatmentKind != "not-subject" {
			s.right(cols[1], pdf.Regular, bodySize, esDecimal(t.Rate, 0))
			s.right(cols[2], pdf.Regular, bodySize, euros(t.Amount))
		}
		if surcharge && !t.SurchargeRate.IsZero() {
			s.right(cols[3], pdf.Regular, bodySize, esDecimal(t.SurchargeRate, 0))
			s.right(cols[4], pdf.Regular, bodySize, euros(t.SurchargeAmount))
		}
		s.y += lineHeight
	}
	s.y += 10

	// Los totales, a la derecha.
	totals := [][2]string{{txtTotalBase, euros(st.Taxes.Net)}, {txtTotalTax, euros(st.Taxes.Tax)}}
	if surcharge {
		totals = append(totals, [2]string{txtTotalSurcharge, euros(st.Taxes.Surcharge)})
	}
	s.need(float64(len(totals)+2) * lineHeight)
	for _, t := range totals {
		s.right(priceRight+60, pdf.Regular, bodySize, t[0])
		s.right(right, pdf.Regular, bodySize, t[1])
		s.y += lineHeight
	}
	s.page.Line(priceRight, s.y-8, right, s.y-8, 0.5)
	s.y += 4
	s.right(priceRight+60, pdf.Bold, 11, txtTotal)
	s.right(right, pdf.Bold, 11, euros(st.Taxes.Total()))
	s.y += 2 * lineHeight

	// La causa de cada exención o no sujeción. Las referencias a la Ley del IVA no valen para el
	// IGIC: en Canarias sólo se da la clave.
	for _, m := range mentions {
		text := fmt.Sprintf(m.format, m.code)
		if cause, ok := exemptionCauses[m.code]; ok && !igic {
			text += " (" + cause + ")"
		}
		s.paragraph(left, right-left, pdf.Regular, text+".")
	}

	// El pie de cada página: su número.
	n := s.doc.Pages()
	for k := range n {
		s.doc.Page(k).TextRight(right, 815, pdf.Regular, smallSize, fmt.Sprintf(txtPage, k+1, n))
	}
	return s.doc.Bytes()
}

// styled es una línea de texto con su fuente y su tamaño.
type styled struct {
	font pdf.Font
	size float64
	text string
}

// partyLines son las líneas de una de las partes: nombre, NIF y domicilio; lo que falte no sale.
func partyLines(label string, id domain.Identity) []styled {
	out := []styled{{pdf.Bold, smallSize, label}}
	for _, l := range pdf.Wrap(pdf.Bold, bodySize, columnWidth, id.Name) {
		out = append(out, styled{pdf.Bold, bodySize, l})
	}
	out = append(out, styled{pdf.Regular, bodySize, txtNIF + ": " + id.NIF})
	a := id.Address
	place := strings.TrimSpace(a.PostalCode + " " + a.Locality)
	for _, t := range []string{a.Street, place, a.Region, a.Country} {
		for _, l := range pdf.Wrap(pdf.Regular, bodySize, columnWidth, t) {
			out = append(out, styled{pdf.Regular, bodySize, l})
		}
	}
	return out
}

// esDate escribe una fecha como dd/mm/aaaa.
func esDate(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%02d/%02d/%04d", d.Day(), int(d.Month()), d.Year())
}

// euros escribe un importe al estilo español con dos decimales: 1.234,56 €.
func euros(d vocab.Decimal) string { return esDecimal(d.Round(2), 2) + " " + txtEuro }

// esDecimal escribe d al estilo español (miles con punto, decimales con coma) con sus decimales
// significativos, y al menos minPlaces: 2,5; 21; 19,9990 con 2 sale 19,999.
func esDecimal(d vocab.Decimal, minPlaces int) string {
	text := vocab.StringMinPlaces(d, minPlaces)
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	whole, frac, _ := strings.Cut(text, ".")
	var b strings.Builder
	for k, c := range whole {
		if k > 0 && (len(whole)-k)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if frac != "" {
		b.WriteString("," + frac)
	}
	return sign + b.String()
}
