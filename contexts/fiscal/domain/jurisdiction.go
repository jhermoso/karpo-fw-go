package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Los impuestos indirectos se calculan por jurisdicción (decisión aprobada, BACKLOG §6): el núcleo
// de Fiscal no depende del país; cada país aporta una Jurisdiction con sus impuestos, reglas,
// redondeo y regímenes, y cada régimen o sector es una estrategia dentro de ella. Un país nuevo es
// una Jurisdiction nueva, registrada sin tocar el núcleo.

// Assessment es lo que necesita una jurisdicción para calcular los impuestos de un documento.
type Assessment struct {
	Taxpayer *Taxpayer // el vendedor: su territorio y su régimen
	Date     vocab.Date
	// EquivalenceSurcharge: el cliente está en el régimen de recargo de equivalencia (comercio
	// minorista español).
	EquivalenceSurcharge bool
	Lines                []TaxableLine
}

// TaxableLine es una base imponible con su código de impuesto o tratamiento. Las bases llegan
// redondeadas a céntimos.
type TaxableLine struct {
	Ref       string
	Base      vocab.Decimal
	TaxCode   string // código de tipo del catálogo (p. ej. G21); vacío cuando el tratamiento es exento o no sujeto
	Treatment string // código de tratamiento del catálogo; vacío: sujeto al impuesto
}

// Breakdown es el desglose de impuestos de un documento: una línea por código de impuesto y
// tratamiento.
type Breakdown struct {
	Lines     []BreakdownLine
	Net       vocab.Decimal
	Tax       vocab.Decimal
	Surcharge vocab.Decimal
}

// BreakdownLine es la base y la cuota de un tipo (o de un tratamiento exento o no sujeto).
type BreakdownLine struct {
	TaxType         TaxType // cero para bases exentas y no sujetas
	TaxCode         string
	Treatment       string
	TreatmentKind   TreatmentKind
	Rate            vocab.Decimal
	Base            vocab.Decimal
	Amount          vocab.Decimal
	SurchargeRate   vocab.Decimal
	SurchargeAmount vocab.Decimal
}

// RateBook da a una jurisdicción las entradas del catálogo vigentes.
type RateBook interface {
	RateOn(ctx context.Context, t TaxType, in Territory, code string, on vocab.Date) (*TaxRate, bool, error)
	Treatment(ctx context.Context, in Territory, code string) (*Treatment, bool, error)
}

// Jurisdiction calcula los impuestos indirectos de un país.
type Jurisdiction interface {
	Country() string
	Calculate(ctx context.Context, a Assessment, book RateBook) (Breakdown, error)
}

// Country es el país de un contribuyente. Todos los territorios modelados hoy son españoles; cuando llegue un
// segundo país, el territorio pasa a su jurisdicción (BACKLOG §6).
func (t *Taxpayer) Country() string { return "ES" }
