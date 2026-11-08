package host

import (
	"context"
	"fmt"

	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bildomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	pcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	prdcontracts "github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
)

// BillingSettings dice a Billing los ajustes de facturación de una empresa (docs/DECISIONES-COMUNES.md,
// D-144 y el precio automático de D-148): los que guarda en su perfil de Parties. Lo que no guarda es lo de siempre: una sola vez al
// final, el precio con 4 decimales, la regla fija del código de serie y los vencimientos después,
// del evento. Se lee siempre: un cambio vale desde el borrador, la serie o la emisión siguientes.
type BillingSettings struct {
	Profiles pcontracts.CompanyProfiles
}

// ProductsSettings dice a Products la tarifa general de una empresa (D-148): el ajuste
// default_price_list de su perfil en Parties. Se lee en cada cotización: un cambio vale desde la
// siguiente.
type ProductsSettings struct {
	Profiles pcontracts.CompanyProfiles
}

// DefaultPriceList implementa products/application.CompanySettings.
func (p ProductsSettings) DefaultPriceList(ctx context.Context, company string) (string, error) {
	st, err := p.Profiles.Settings(ctx, company)
	if err != nil {
		return "", fmt.Errorf("products settings: company %s: %w", company, err)
	}
	return st.DefaultPriceList, nil
}

// DefaultPriceLists comprueba en Products la tarifa general que una empresa guarda en su perfil de
// Parties (D-148): que exista, sea suya y esté activa. Parties se compone antes que Products, así
// que Lists se pone después; mientras no está, todo vale (como sin puerto).
type DefaultPriceLists struct {
	Lists prdcontracts.PriceLists
}

// UsableDefaultPriceList implementa parties/application.PriceListCheck.
func (d *DefaultPriceLists) UsableDefaultPriceList(ctx context.Context, company, priceList string) (bool, error) {
	if d.Lists == nil {
		return true, nil
	}
	ref, ok, err := d.Lists.PriceList(ctx, priceList)
	if err != nil {
		return false, fmt.Errorf("default price list %s: %w", priceList, err)
	}
	return ok && ref.Company == company && ref.Active, nil
}

// Settings implementa billing/application.Settings.
func (b BillingSettings) Settings(ctx context.Context, company string) (bilapp.CompanySettings, error) {
	st, err := b.Profiles.Settings(ctx, company)
	if err != nil {
		return bilapp.CompanySettings{}, fmt.Errorf("billing settings: company %s: %w", company, err)
	}
	pattern, err := b.Profiles.CodePattern(ctx, company, pdomain.CodeKindInvoiceSeries)
	if err != nil {
		return bilapp.CompanySettings{}, fmt.Errorf("billing settings: company %s: %w", company, err)
	}
	calc := bildomain.DefaultCalculation()
	if st.LineRounding != "" {
		calc.Rounding = bildomain.LineRounding(st.LineRounding)
	}
	if st.PriceDecimals != nil {
		calc.PriceDecimals = *st.PriceDecimals
	}
	return bilapp.CompanySettings{Calculation: calc, SeriesPattern: pattern,
		InstallmentsWithIssue: st.InstallmentsTiming == pdomain.InstallmentsWithIssue, ManualPricing: !st.AutomaticPricing}, nil
}
