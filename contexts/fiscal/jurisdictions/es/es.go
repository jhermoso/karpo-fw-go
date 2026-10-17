// Package es es la jurisdicción española del contexto Fiscal. Esta fase sólo implementa el régimen
// general: base por tipo agregada sobre todo el documento, cuota por tipo redondeada al céntimo
// (la mitad, alejándose de cero), bases exentas y no sujetas, y el recargo de equivalencia. El
// resto (inversión del sujeto pasivo, regímenes especiales por sector, operaciones
// intracomunitarias y con el extranjero, criterio de caja, prorrata…) está en BACKLOG §6.
package es

import (
	"context"
	"strconv"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Spain implementa domain.Jurisdiction.
type Spain struct{}

var _ domain.Jurisdiction = Spain{}

// Country implementa domain.Jurisdiction.
func (Spain) Country() string { return "ES" }

// taxOf devuelve el impuesto indirecto que se aplica en un territorio.
func taxOf(t domain.Territory) domain.TaxType {
	switch t {
	case domain.Canaries:
		return domain.IGIC
	case domain.CeutaMelilla:
		return domain.IPSI
	}
	return domain.VAT
}

var hundred = vocab.DecimalFromInt(100)

// Calculate implementa domain.Jurisdiction.
func (Spain) Calculate(ctx context.Context, a domain.Assessment, book domain.RateBook) (domain.Breakdown, error) {
	if a.Taxpayer == nil {
		return domain.Breakdown{}, fw.Violation("fiscal.no_taxpayer", "the seller has no fiscal profile")
	}
	territory := a.Taxpayer.Terms().Territory
	tax := taxOf(territory)
	zero := vocab.DecimalFromInt(0)
	type key struct{ code, treatment string }
	var order []key
	groups := map[key]*domain.BreakdownLine{}
	for i, l := range a.Lines {
		code, treat := domain.NormalizeCode(l.TaxCode), domain.NormalizeCode(l.Treatment)
		if !l.Base.Equal(l.Base.Round(2)) {
			return domain.Breakdown{}, fw.Violation("fiscal.base_not_rounded", "taxable bases must be rounded to the cent")
		}
		k := key{code, treat}
		g, ok := groups[k]
		if !ok {
			g = &domain.BreakdownLine{TaxCode: code, Treatment: treat, TreatmentKind: domain.Subject, Base: zero, Amount: zero, Rate: zero,
				SurchargeRate: zero, SurchargeAmount: zero}
			if treat != "" {
				t, found, err := book.Treatment(ctx, territory, treat)
				if err != nil {
					return domain.Breakdown{}, err
				}
				if !found || !t.State().Active {
					return domain.Breakdown{}, fw.Violation("fiscal.unknown_treatment", "line "+ref(l, i)+": unknown tax treatment "+treat)
				}
				g.TreatmentKind = t.State().Kind
			}
			if g.TreatmentKind == domain.Subject {
				if code == "" {
					return domain.Breakdown{}, fw.Violation("fiscal.missing_tax_code", "line "+ref(l, i)+": a subject line needs a tax code")
				}
				r, found, err := book.RateOn(ctx, tax, territory, code, a.Date)
				if err != nil {
					return domain.Breakdown{}, err
				}
				if !found {
					return domain.Breakdown{}, fw.Violation("fiscal.unknown_rate", "line "+ref(l, i)+": no rate "+code+" in force on "+a.Date.String())
				}
				s := r.State()
				g.TaxType, g.Rate = s.Type, s.Rate
				if a.EquivalenceSurcharge {
					g.SurchargeRate = s.Surcharge
				}
			} else if code != "" {
				return domain.Breakdown{}, fw.Violation("fiscal.tax_code_on_exempt", "line "+ref(l, i)+": an exempt or not subject line has no tax code")
			}
			groups[k] = g
			order = append(order, k)
		}
		g.Base = g.Base.Add(l.Base)
	}
	out := domain.Breakdown{Net: zero, Tax: zero, Surcharge: zero}
	for _, k := range order {
		g := groups[k]
		// La cuota se calcula una vez por tipo sobre la base agregada, no línea a línea.
		g.Amount = g.Base.Mul(g.Rate).Div(hundred).Round(2)
		g.SurchargeAmount = g.Base.Mul(g.SurchargeRate).Div(hundred).Round(2)
		out.Lines = append(out.Lines, *g)
		out.Net = out.Net.Add(g.Base)
		out.Tax = out.Tax.Add(g.Amount)
		out.Surcharge = out.Surcharge.Add(g.SurchargeAmount)
	}
	return out, nil
}

func ref(l domain.TaxableLine, i int) string {
	if strings.TrimSpace(l.Ref) != "" {
		return l.Ref
	}
	return "#" + strconv.Itoa(i+1)
}
