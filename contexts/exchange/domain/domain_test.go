package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

var (
	dec     = vocab.MustDecimal
	company = domain.OrganizationID{UUID: fw.NewUUID()}
	may     = vocab.MustDate(2026, 5, 4)
)

func currency(t *testing.T, code, rate, facial string) *domain.Currency {
	t.Helper()
	c, err := domain.ReconstituteCurrency(domain.NewCurrencyID(), domain.CurrencyState{Company: company, Code: code, Name: code, Rate: dec(rate), RateOn: may,
		Facial: dec(facial)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func margin(t *testing.T, cur string, kind domain.MarginKind, v1, v2, v3 string) *domain.Margin {
	t.Helper()
	m, err := domain.ReconstituteMargin(domain.NewMarginID(), domain.MarginState{Company: company, Currency: cur, Segment: "web", Kind: kind,
		Values: [3]vocab.Decimal{dec(v1), dec(v2), dec(v3)}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The numbers of the C# quote tests: USD at 0.92 with margins of 1.5, 2 and 2.5 %.
func TestQuote_RateMarginAndNotes(t *testing.T) {
	usd, set := currency(t, "usd", "0.92", "0"), domain.DefaultSettings(company)
	m := margin(t, "USD", domain.Percent, "1.5", "2", "2.5")
	q, err := domain.Quote(usd, m, set, "web", dec("500"))
	if err != nil || !q.OfferedRate.Equal(dec("0.9384")) || !q.Eur.Equal(dec("469.20")) || q.Level != 2 || !q.MarginValue.Equal(dec("2")) || q.NoMargin ||
		q.Segment != "WEB" || q.Currency != "USD" || !q.Delivered.Equal(dec("500")) {
		t.Fatalf("second level: %+v %v", q, err)
	}
	set.Level = 3
	if q, _ := domain.Quote(usd, m, set, "WEB", dec("500")); !q.OfferedRate.Equal(dec("0.943")) || !q.Eur.Equal(dec("471.50")) {
		t.Fatalf("third level: %+v", q)
	}
	// Whole notes of 5: 123 becomes 120, and 120 × 0.9384 = 112.608.
	set.Level = 2
	if q, _ := domain.Quote(currency(t, "USD", "0.92", "5"), m, set, "WEB", dec("123")); !q.Delivered.Equal(dec("120")) || !q.Eur.Equal(dec("112.61")) {
		t.Fatalf("notes: %+v", q)
	}
	// Without a margin for the segment the reference rate is offered, and it is said.
	if q, err := domain.Quote(currency(t, "GBP", "1.17", "5"), nil, set, "WEB", dec("500")); err != nil || !q.OfferedRate.Equal(dec("1.17")) ||
		!q.Eur.Equal(dec("585.00")) || !q.NoMargin {
		t.Fatalf("no margin: %+v %v", q, err)
	}
	if got := domain.RoundDownToFacial(dec("3"), dec("5")); !got.Equal(dec("3")) {
		t.Fatalf("under one note: %s", got)
	}
	// Absolute and pip margins, and their percentage.
	jpy := currency(t, "JPY", "0.0062", "1000")
	if q, _ := domain.Quote(jpy, margin(t, "JPY", domain.Pip, "1", "2", "3"), set, "WEB", dec("25500")); !q.OfferedRate.Equal(dec("0.0064")) ||
		!q.Delivered.Equal(dec("25000")) || !q.Eur.Equal(dec("160.00")) || !q.MarginPercent.Equal(dec("3.225806")) {
		t.Fatalf("pip: %+v", q)
	}
	if q, _ := domain.Quote(usd, margin(t, "USD", domain.Absolute, "0.01", "0.02", "0.03"), set, "WEB", dec("100")); !q.OfferedRate.Equal(dec("0.94")) ||
		!q.Eur.Equal(dec("94.00")) || !q.MarginPercent.Equal(dec("2.173913")) {
		t.Fatalf("absolute: %+v", q)
	}
	for code, f := range map[string]func() (domain.Quotation, error){
		"exchange.amount": func() (domain.Quotation, error) { return domain.Quote(usd, m, set, "WEB", dec("0")) },
		"exchange.no_rate": func() (domain.Quotation, error) {
			c, _ := domain.ReconstituteCurrency(domain.NewCurrencyID(), domain.CurrencyState{Company: company, Code: "CHF", Name: "Franco"})
			return domain.Quote(c, nil, set, "WEB", dec("100"))
		},
		"exchange.currency_blocked": func() (domain.Quotation, error) {
			c := currency(t, "CHF", "1.05", "10")
			_ = c.Change("Franco", dec("10"), false, true)
			return domain.Quote(c, nil, set, "WEB", dec("100"))
		},
		"exchange.crypto_disabled": func() (domain.Quotation, error) {
			c := currency(t, "BTC", "60000", "0")
			_ = c.Change("Bitcoin", dec("0"), true, false)
			return domain.Quote(c, nil, set, "WEB", dec("1"))
		},
	} {
		if _, err := f(); !isViolation(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
}

func TestPricing_Rules(t *testing.T) {
	if _, err := domain.ReconstituteCurrency(domain.NewCurrencyID(), domain.CurrencyState{Company: company, Code: "EUR", Name: "Euro"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("the euro is not exchanged for itself: %v", err)
	}
	usd := currency(t, "USD", "0.92", "5")
	if err := usd.SetRate(dec("0"), may); !isViolation(err, "exchange.rate") {
		t.Fatalf("zero rate: %v", err)
	}
	if err := usd.SetRate(dec("0.93"), vocab.MustDate(2026, 5, 3)); !isViolation(err, "exchange.rate_date") {
		t.Fatalf("an older rate: %v", err)
	}
	if err := usd.SetRate(dec("0.9312345"), may); !isViolation(err, "exchange.rate") {
		t.Fatalf("seven decimals: %v", err)
	}
	if err := usd.SetRate(dec("0.93"), vocab.MustDate(2026, 5, 5)); err != nil || !usd.State().Rate.Equal(dec("0.93")) {
		t.Fatal(err)
	}
	if _, err := domain.ReconstituteMargin(domain.NewMarginID(), domain.MarginState{Company: company, Currency: "USD", Segment: "WEB", Kind: "tip"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("margin kind: %v", err)
	}
	set, err := domain.ReconstituteSettings(domain.NewSettingsID(), domain.DefaultSettings(company))
	if err != nil || set.State().Level != 2 || set.State().PromotionMode != "optional" || !set.State().ValidatePromotion || set.State().ExpiryHours != 4 {
		t.Fatalf("defaults: %+v %v", set, err)
	}
	for i, bad := range []domain.SettingsState{{Level: 4, PromotionMode: "optional", ExpiryHours: 4}, {Level: 2, PromotionMode: "rare", ExpiryHours: 4},
		{Level: 2, PromotionMode: "optional", ExpiryHours: 0}} {
		if err := set.Change(bad); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("settings %d: %v", i, err)
		}
	}
	if err := set.Change(domain.SettingsState{Level: 3, PromotionMode: "required", ExpiryHours: 24}); err != nil || set.State().Company != company || set.State().Level != 3 {
		t.Fatalf("change: %v", err)
	}
}

func TestReservation_Lifecycle(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	pickup := time.Date(2026, 5, 6, 17, 0, 0, 0, time.UTC)
	usd, set := currency(t, "USD", "0.92", "0"), domain.DefaultSettings(company)
	q, _ := domain.Quote(usd, nil, set, "WEB", dec("500"))
	eurLine := domain.Quotation{Currency: "GBP", Amount: dec("300"), Delivered: dec("300"), BaseRate: dec("1"), OfferedRate: dec("1"), MarginKind: domain.Percent,
		MarginValue: dec("0"), Eur: dec("300")}
	req := func() domain.Request {
		return domain.Request{Company: company, Customer: domain.PartyID{UUID: fw.NewUUID()}, Channel: " Web ", Segment: "web", Pickup: pickup,
			Lines: []domain.RequestLine{{Quotation: q}, {Quotation: eurLine}}}
	}
	register := func(r domain.Request) (*domain.Reservation, error) {
		return domain.RegisterReservation(domain.NewReservationID(), r, 4, now)
	}
	past := req()
	past.Pickup = now
	if _, err := register(past); !isViolation(err, "exchange.pickup_past") {
		t.Fatalf("pickup: %v", err)
	}
	bad := []func(*domain.Request){
		func(r *domain.Request) { r.Channel = "fax" },
		func(r *domain.Request) { r.Lines = nil },
		func(r *domain.Request) { r.Lines[1].Currency = "USD" },
		func(r *domain.Request) { r.Lines[0].Collector = true },
		func(r *domain.Request) { r.PromotionCode = strings.Repeat("X", 16) },
		func(r *domain.Request) { r.Customer = domain.PartyID{} },
	}
	for i, f := range bad {
		r := req()
		f(&r)
		if _, err := register(r); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	// The C# example: USD 500 at 0.92 and 300 at 1.00 make 760.00, waiting four hours after pickup.
	r, err := register(req())
	if err != nil {
		t.Fatal(err)
	}
	s := r.State()
	if !strings.HasPrefix(s.Reference, "RES-2026-") || len(s.Reference) != 20 || s.Status != domain.Registered || s.Channel != "web" || s.Segment != "WEB" ||
		!s.ExpiresAt.Equal(pickup.Add(4*time.Hour)) || !r.Total().Equal(dec("760.00")) || !s.Lines[0].Eur.Equal(dec("460.00")) || s.Lines[1].No != 2 {
		t.Fatalf("registered: %+v total %s", s, r.Total())
	}
	if _, err := r.Advance(domain.Completed, now); !isViolation(err, "exchange.transition") {
		t.Fatalf("collected before being verified: %v", err)
	}
	if changed, err := r.Advance(domain.EmailVerified, now); err != nil || !changed {
		t.Fatal(err)
	}
	if changed, err := r.Advance(domain.EmailVerified, now); err != nil || changed {
		t.Fatalf("the same status again changes nothing: %v %v", changed, err)
	}
	if _, err := r.Advance(domain.Notified, now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Advance(domain.Completed, pickup.Add(4*time.Hour)); !isViolation(err, "exchange.expired") {
		t.Fatalf("collected too late: %v", err)
	}
	if changed, err := r.Advance(domain.Completed, pickup); err != nil || !changed || r.Active() || len(r.State().History) != 4 {
		t.Fatalf("collected: %v", err)
	}
	if _, err := r.Cancel(pickup, ""); !isViolation(err, "exchange.transition") {
		t.Fatalf("cancel what was collected: %v", err)
	}
	if r.Expire(pickup.Add(48 * time.Hour)) {
		t.Fatal("what was collected does not expire")
	}

	c, _ := register(req())
	if changed, err := c.Cancel(now, "El cliente desiste"); err != nil || !changed || c.State().Status != domain.Cancelled {
		t.Fatal(err)
	}
	if changed, err := c.Cancel(now, ""); err != nil || changed {
		t.Fatalf("cancelled once: %v %v", changed, err)
	}
	e, _ := register(req())
	if e.Expire(pickup.Add(3*time.Hour)) || !e.Expire(pickup.Add(4*time.Hour)) || e.State().Status != domain.Expired {
		t.Fatalf("expiry: %+v", e.State())
	}
	if _, err := e.Cancel(pickup.Add(5*time.Hour), ""); !isViolation(err, "exchange.transition") {
		t.Fatalf("cancel what expired: %v", err)
	}
}
