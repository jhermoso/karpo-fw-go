package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SetCurrency adds a currency to those a company exchanges, or changes it.
type SetCurrency struct {
	Company string `json:"company"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Facial  string `json:"facial,omitempty"`
	Crypto  bool   `json:"crypto,omitempty"`
	Blocked bool   `json:"blocked,omitempty"`
}

// SetRate fixes the reference rate of a currency: euros for one unit.
type SetRate struct {
	Company string     `json:"company"`
	Code    string     `json:"code"`
	Rate    string     `json:"rate"`
	On      vocab.Date `json:"on,omitzero"` // today by default
}

// ListCurrencies lists the currencies of a company.
type ListCurrencies struct{ Company string }

// SetMargin sets the margin of a currency for a segment: one value per level of price.
type SetMargin struct {
	Company  string `json:"company"`
	Currency string `json:"currency"`
	Segment  string `json:"segment,omitempty"` // WEB by default
	Kind     string `json:"kind"`
	Level1   string `json:"level1"`
	Level2   string `json:"level2"`
	Level3   string `json:"level3"`
}

// ListMargins lists the margins of a company.
type ListMargins struct{ Company string }

// GetSettings returns the settings of a company.
type GetSettings struct{ Company string }

// SetSettings replaces the settings of a company.
type SetSettings struct {
	Company           string `json:"company"`
	Level             int    `json:"level"`
	PromotionMode     string `json:"promotionMode"`
	ValidatePromotion bool   `json:"validatePromotion"`
	ExpiryHours       int    `json:"expiryHours"`
	CryptoEnabled     bool   `json:"cryptoEnabled,omitempty"`
}

// GetQuote prices an amount of a currency.
type GetQuote struct{ Company, Currency, Segment, Amount string }

// CurrencyDTO is the transport form of a currency.
type CurrencyDTO struct {
	ID      string `json:"id"`
	Company string `json:"company"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Rate    string `json:"rate,omitempty"`
	RateOn  string `json:"rateOn,omitempty"`
	Facial  string `json:"facial"`
	Crypto  bool   `json:"crypto"`
	Blocked bool   `json:"blocked"`
	Version int64  `json:"version"`
}

func currencyDTO(c *domain.Currency) CurrencyDTO {
	s := c.State()
	d := CurrencyDTO{ID: c.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Facial: s.Facial.StringFixed(2), Crypto: s.Crypto,
		Blocked: s.Blocked, Version: c.Version()}
	if s.Rate.IsPositive() {
		d.Rate, d.RateOn = s.Rate.StringFixed(6), s.RateOn.String()
	}
	return d
}

// MarginDTO is the transport form of a margin.
type MarginDTO struct {
	ID       string `json:"id"`
	Company  string `json:"company"`
	Currency string `json:"currency"`
	Segment  string `json:"segment"`
	Kind     string `json:"kind"`
	Level1   string `json:"level1"`
	Level2   string `json:"level2"`
	Level3   string `json:"level3"`
	Version  int64  `json:"version"`
}

func marginDTO(m *domain.Margin) MarginDTO {
	s := m.State()
	return MarginDTO{ID: m.ID().String(), Company: s.Company.String(), Currency: s.Currency, Segment: s.Segment, Kind: string(s.Kind),
		Level1: s.Values[0].String(), Level2: s.Values[1].String(), Level3: s.Values[2].String(), Version: m.Version()}
}

// SettingsDTO is the transport form of the settings. Defaults is true until the company changes
// them.
type SettingsDTO struct {
	Company           string `json:"company"`
	Level             int    `json:"level"`
	PromotionMode     string `json:"promotionMode"`
	ValidatePromotion bool   `json:"validatePromotion"`
	ExpiryHours       int    `json:"expiryHours"`
	CryptoEnabled     bool   `json:"cryptoEnabled"`
	Defaults          bool   `json:"defaults"`
	Version           int64  `json:"version"`
}

func settingsDTO(st domain.SettingsState, own *domain.Settings) SettingsDTO {
	d := SettingsDTO{Company: st.Company.String(), Level: st.Level, PromotionMode: st.PromotionMode, ValidatePromotion: st.ValidatePromotion,
		ExpiryHours: st.ExpiryHours, CryptoEnabled: st.CryptoEnabled, Defaults: own == nil}
	if own != nil {
		d.Version = own.Version()
	}
	return d
}

// QuoteDTO is the transport form of a quotation.
type QuoteDTO struct {
	Currency      string `json:"currency"`
	Segment       string `json:"segment"`
	Amount        string `json:"amount"`
	BaseRate      string `json:"baseRate"`
	RateOn        string `json:"rateOn"`
	OfferedRate   string `json:"offeredRate"`
	MarginKind    string `json:"marginKind"`
	Level         int    `json:"level"`
	MarginValue   string `json:"marginValue"`
	MarginPercent string `json:"marginPercent"`
	Facial        string `json:"facial"`
	Delivered     string `json:"delivered"`
	Eur           string `json:"eur"`
	NoMargin      bool   `json:"noMargin"`
}

func quoteDTO(q domain.Quotation) QuoteDTO {
	return QuoteDTO{Currency: q.Currency, Segment: q.Segment, Amount: q.Amount.StringFixed(2), BaseRate: q.BaseRate.StringFixed(6), RateOn: q.RateOn.String(),
		OfferedRate: q.OfferedRate.StringFixed(6), MarginKind: string(q.MarginKind), Level: q.Level, MarginValue: q.MarginValue.String(),
		MarginPercent: q.MarginPercent.String(), Facial: q.Facial.StringFixed(2), Delivered: q.Delivered.StringFixed(2), Eur: q.Eur.StringFixed(2),
		NoMargin: q.NoMargin}
}

func (s service) wirePricing(svc *Service) {
	d := s.Deps
	svc.SetCurrency = changing(d.UoW, PermPricingUpdate, func(ctx context.Context, c SetCurrency) (CurrencyDTO, error) {
		var v fw.Validation
		facial := parseDecimal(&v, "facial", c.Facial)
		org, err := company(ctx, &v, c.Company, true)
		if err != nil {
			return CurrencyDTO{}, err
		}
		found, err := d.Currencies.Find(ctx, spec.And(domain.CurFieldCompany.Eq(org), domain.CurFieldCode.Eq(domain.NormalizeCode(c.Code))))
		if err != nil {
			return CurrencyDTO{}, err
		}
		if len(found) == 0 {
			cur, err := domain.ReconstituteCurrency(domain.NewCurrencyID(), domain.CurrencyState{Company: org, Code: c.Code, Name: c.Name, Facial: facial,
				Crypto: c.Crypto, Blocked: c.Blocked})
			if err != nil {
				return CurrencyDTO{}, err
			}
			if err := s.currencies.Create(ctx, cur); err != nil {
				return CurrencyDTO{}, err
			}
			return currencyDTO(cur), nil
		}
		cur, err := s.currencies.Update(ctx, found[0].ID(), func(_ context.Context, cur *domain.Currency) error {
			return cur.Change(c.Name, facial, c.Crypto, c.Blocked)
		})
		if err != nil {
			return CurrencyDTO{}, err
		}
		return currencyDTO(cur), nil
	})
	svc.SetRate = changing(d.UoW, PermPricingUpdate, func(ctx context.Context, c SetRate) (CurrencyDTO, error) {
		var v fw.Validation
		rate := parseDecimal(&v, "rate", c.Rate)
		org, err := company(ctx, &v, c.Company, true)
		if err != nil {
			return CurrencyDTO{}, err
		}
		cur, err := s.currency(ctx, org, c.Code)
		if err != nil {
			return CurrencyDTO{}, err
		}
		today := vocab.DateOf(fw.Now())
		on := c.On
		if on.IsZero() {
			on = today
		}
		if on.After(today) {
			return CurrencyDTO{}, fw.Violation("exchange.rate_date", "a rate is not set for a day to come")
		}
		cur, err = s.currencies.Update(ctx, cur.ID(), func(_ context.Context, cur *domain.Currency) error { return cur.SetRate(rate, on) })
		if err != nil {
			return CurrencyDTO{}, err
		}
		return currencyDTO(cur), nil
	})
	svc.ListCurrencies = guard(PermPricingRead, func(ctx context.Context, q ListCurrencies) ([]CurrencyDTO, error) {
		var v fw.Validation
		org, err := company(ctx, &v, q.Company, false)
		if err != nil {
			return nil, err
		}
		cs, err := d.Currencies.Find(ctx, domain.CurFieldCompany.Eq(org), domain.CurFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []CurrencyDTO{}
		for _, c := range cs {
			out = append(out, currencyDTO(c))
		}
		return out, nil
	})

	svc.SetMargin = changing(d.UoW, PermPricingUpdate, func(ctx context.Context, c SetMargin) (MarginDTO, error) {
		var v fw.Validation
		values := [domain.MaxLevel]vocab.Decimal{parseDecimal(&v, "level1", c.Level1), parseDecimal(&v, "level2", c.Level2), parseDecimal(&v, "level3", c.Level3)}
		org, err := company(ctx, &v, c.Company, true)
		if err != nil {
			return MarginDTO{}, err
		}
		segment := c.Segment
		if strings.TrimSpace(segment) == "" {
			segment = domain.DefaultSegment
		}
		if _, err := s.currency(ctx, org, c.Currency); err != nil {
			return MarginDTO{}, err
		}
		kind := domain.MarginKind(strings.TrimSpace(c.Kind))
		existing, err := s.margin(ctx, org, c.Currency, segment)
		if err != nil {
			return MarginDTO{}, err
		}
		if existing == nil {
			m, err := domain.ReconstituteMargin(domain.NewMarginID(), domain.MarginState{Company: org, Currency: c.Currency, Segment: segment, Kind: kind, Values: values})
			if err != nil {
				return MarginDTO{}, err
			}
			if err := s.margins.Create(ctx, m); err != nil {
				return MarginDTO{}, err
			}
			return marginDTO(m), nil
		}
		m, err := s.margins.Update(ctx, existing.ID(), func(_ context.Context, m *domain.Margin) error { return m.Change(kind, values) })
		if err != nil {
			return MarginDTO{}, err
		}
		return marginDTO(m), nil
	})
	svc.ListMargins = guard(PermPricingRead, func(ctx context.Context, q ListMargins) ([]MarginDTO, error) {
		var v fw.Validation
		org, err := company(ctx, &v, q.Company, false)
		if err != nil {
			return nil, err
		}
		ms, err := d.Margins.Find(ctx, domain.MrgFieldCompany.Eq(org), domain.MrgFieldCur.Asc(), domain.MrgFieldSegment.Asc())
		if err != nil {
			return nil, err
		}
		out := []MarginDTO{}
		for _, m := range ms {
			out = append(out, marginDTO(m))
		}
		return out, nil
	})

	svc.GetSettings = guard(PermPricingRead, func(ctx context.Context, q GetSettings) (SettingsDTO, error) {
		var v fw.Validation
		org, err := company(ctx, &v, q.Company, false)
		if err != nil {
			return SettingsDTO{}, err
		}
		own, st, err := s.settingsOf(ctx, org)
		if err != nil {
			return SettingsDTO{}, err
		}
		return settingsDTO(st, own), nil
	})
	svc.SetSettings = changing(d.UoW, PermPricingUpdate, func(ctx context.Context, c SetSettings) (SettingsDTO, error) {
		var v fw.Validation
		org, err := company(ctx, &v, c.Company, true)
		if err != nil {
			return SettingsDTO{}, err
		}
		st := domain.SettingsState{Company: org, Level: c.Level, PromotionMode: strings.TrimSpace(c.PromotionMode), ValidatePromotion: c.ValidatePromotion,
			ExpiryHours: c.ExpiryHours, CryptoEnabled: c.CryptoEnabled}
		own, _, err := s.settingsOf(ctx, org)
		if err != nil {
			return SettingsDTO{}, err
		}
		if own == nil {
			if own, err = domain.ReconstituteSettings(domain.NewSettingsID(), st); err != nil {
				return SettingsDTO{}, err
			}
			if err := s.settings.Create(ctx, own); err != nil {
				return SettingsDTO{}, err
			}
			return settingsDTO(own.State(), own), nil
		}
		own, err = s.settings.Update(ctx, own.ID(), func(_ context.Context, x *domain.Settings) error { return x.Change(st) })
		if err != nil {
			return SettingsDTO{}, err
		}
		return settingsDTO(own.State(), own), nil
	})

	svc.Quote = guard(PermReservationRead, func(ctx context.Context, q GetQuote) (QuoteDTO, error) {
		var v fw.Validation
		amount := parseDecimal(&v, "amount", q.Amount)
		org, err := company(ctx, &v, q.Company, false)
		if err != nil {
			return QuoteDTO{}, err
		}
		_, set, err := s.settingsOf(ctx, org)
		if err != nil {
			return QuoteDTO{}, err
		}
		quotation, err := s.quote(ctx, org, set, q.Currency, q.Segment, amount)
		if err != nil {
			return QuoteDTO{}, err
		}
		return quoteDTO(quotation), nil
	})
}
