package vocab_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isValidation(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected a validation error, got %v", err)
	}
}

func TestName(t *testing.T) {
	n, err := vocab.NewName("  Acme   Corporation ")
	if err != nil || n.String() != "Acme Corporation" {
		t.Fatalf("normalization: %q %v", n, err)
	}
	// Identity is the text: no format enum makes equal names different (C# Name bug).
	if vocab.MustName("Acme") != vocab.MustName(" Acme ") || !vocab.MustName("ACME").EqualFold(vocab.MustName("acme")) {
		t.Fatal("name equality")
	}
	for _, bad := range []string{"", "   ", "a\x00b", string(make([]byte, 201))} {
		_, err := vocab.NewName(bad)
		isValidation(t, err)
	}
	cases := map[string]string{
		"juan DE LA fuente": "Juan de la Fuente", // particles stay lower case (C# AllCapitalized rejected them)
		"McDonald":          "McDonald",          // internal capitals preserved
		"MARÍA JOSÉ":        "María José",
		"o'neill":           "O'neill",
		"de la Rosa":        "De la Rosa", // a leading particle is capitalized
	}
	for in, want := range cases {
		if got := vocab.CapitalizeWords(in); got != want {
			t.Errorf("CapitalizeWords(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmail(t *testing.T) {
	e, err := vocab.NewEmail(" John.Doe@Example.COM ")
	if err != nil {
		t.Fatal(err)
	}
	// LocalPart/Domain derive from the normalized value (C# kept the original casing).
	if e.String() != "john.doe@example.com" || e.LocalPart() != "john.doe" || e.Domain() != "example.com" {
		t.Fatalf("normalization: %s %s %s", e, e.LocalPart(), e.Domain())
	}
	for _, bad := range []string{"", "no-at", "a@b", "John <j@x.com>", "a@-x.com", "a@x..com"} {
		_, err := vocab.NewEmail(bad)
		isValidation(t, err)
	}
}

func TestPhone(t *testing.T) {
	cases := map[string][2]string{
		"+34 600 111 222":   {"34", "600111222"}, // C# read calling code 346
		"0034600111222":     {"34", "600111222"},
		"+1 (212) 555-0100": {"1", "2125550100"},
		"+44 7911 123456":   {"44", "7911123456"},
		"+351 912 345 678":  {"351", "912345678"},
	}
	for in, want := range cases {
		p, err := vocab.NewPhone(in)
		if err != nil || p.CallingCode() != want[0] || p.NationalNumber() != want[1] {
			t.Errorf("NewPhone(%q) = %s/%s %v, want %v", in, p.CallingCode(), p.NationalNumber(), err, want)
		}
	}
	if p, err := vocab.NewPhoneWithDefault("600 111 222", "34"); err != nil || p.String() != "+34600111222" {
		t.Fatalf("default calling code: %s %v", p, err)
	}
	for _, bad := range []string{"", "600111222", "+999 123456", "+34 12", "+34 600-abc"} {
		_, err := vocab.NewPhone(bad)
		isValidation(t, err)
	}
}

func TestURL(t *testing.T) {
	u, err := vocab.NewURL("HTTPS://Example.com/Path?q=1")
	if err != nil || u.String() != "https://example.com/Path?q=1" || u.Parsed().Host != "example.com" {
		t.Fatalf("url: %s %v", u, err)
	}
	for _, bad := range []string{"", "ftp://x.com", "/relative", "https://"} {
		_, err := vocab.NewURL(bad)
		isValidation(t, err)
	}
}

func TestCountryAndCurrency(t *testing.T) {
	if vocab.MustCountryCode("es") != vocab.Spain {
		t.Fatal("country normalization")
	}
	_, err := vocab.NewCountryCode("XX")
	isValidation(t, err)
	if vocab.MustCurrencyCode("jpy").MinorUnits() != 0 || vocab.EUR.MinorUnits() != 2 || vocab.MustCurrencyCode("KWD").MinorUnits() != 3 {
		t.Fatal("minor units")
	}
	_, err = vocab.NewCurrencyCode("ABC")
	isValidation(t, err)
}

func TestMoney(t *testing.T) {
	a := vocab.MustMoney("10.10", "EUR")
	b := vocab.MustMoney("0.20", "EUR")
	sum, err := a.Add(b)
	if err != nil || sum.String() != "10.30 EUR" {
		t.Fatalf("add: %s %v", sum, err)
	}
	if _, err := a.Add(vocab.MustMoney("1", "USD")); !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("mixing currencies must be a rule violation, got %v", err)
	}
	if got := vocab.MustMoney("2.675", "EUR").Round().String(); got != "2.68 EUR" {
		t.Fatalf("round half away from zero: %s", got)
	}
	if got := vocab.MustMoney("1234.5", "JPY").Round().String(); got != "1235 JPY" {
		t.Fatalf("JPY has no minor units: %s", got)
	}

	shares, err := vocab.MustMoney("100.00", "EUR").Allocate(1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	total := vocab.Zero(vocab.EUR)
	for _, s := range shares {
		total, _ = total.Add(s)
	}
	if shares[0].String() != "33.34 EUR" || shares[2].String() != "33.33 EUR" || !total.Equal(vocab.MustMoney("100", "EUR")) {
		t.Fatalf("allocation must not lose cents: %v total %s", shares, total)
	}
	neg, _ := vocab.MustMoney("-0.05", "EUR").Allocate(1, 1)
	if neg[0].String() != "-0.03 EUR" || neg[1].String() != "-0.02 EUR" {
		t.Fatalf("negative allocation: %v", neg)
	}

	raw, _ := json.Marshal(vocab.MustMoney("12.50", "EUR"))
	if string(raw) != `{"amount":"12.5","currency":"EUR"}` {
		t.Fatalf("json: %s", raw)
	}
	var back vocab.Money
	if err := json.Unmarshal(raw, &back); err != nil || !back.Equal(vocab.MustMoney("12.5", "EUR")) {
		t.Fatalf("json round trip: %s %v", back, err)
	}
}

func TestPercentage(t *testing.T) {
	vat := vocab.MustPercentage("21")
	if got := vat.Of(vocab.MustMoney("100", "EUR")).Round().String(); got != "21.00 EUR" {
		t.Fatalf("of: %s", got)
	}
	if got := vat.AddTo(vocab.MustMoney("100", "EUR")).Round().String(); got != "121.00 EUR" {
		t.Fatalf("add to: %s", got)
	}
	// Not clamped to 0-100 (C# clipped silently).
	if !vocab.MustPercentage("150").Within(vocab.DecimalFromInt(0), vocab.DecimalFromInt(200)) ||
		vocab.MustPercentage("150").Within(vocab.DecimalFromInt(0), vocab.DecimalFromInt(100)) {
		t.Fatal("range checks are explicit")
	}
	if !vocab.PercentageFromFraction(vocab.MustDecimal("0.21")).Equal(vat) {
		t.Fatal("fraction")
	}
}

func TestDate(t *testing.T) {
	d := vocab.MustDate(2024, time.January, 31)
	if d.AddMonths(1) != vocab.MustDate(2024, time.February, 29) || d.AddMonths(13) != vocab.MustDate(2025, time.February, 28) {
		t.Fatal("month arithmetic must clamp to the end of month")
	}
	if d.AddDays(1) != vocab.MustDate(2024, time.February, 1) || d.DaysUntil(vocab.MustDate(2024, time.March, 1)) != 30 {
		t.Fatal("day arithmetic")
	}
	if _, err := vocab.NewDate(2023, time.February, 29); err == nil {
		t.Fatal("invalid calendar date accepted")
	}
	p, err := vocab.ParseDate("2025-12-24")
	if err != nil || p.String() != "2025-12-24" || p.Weekday() != time.Wednesday {
		t.Fatalf("parse: %s %v", p, err)
	}
	raw, _ := json.Marshal(struct{ D vocab.Date }{p})
	if string(raw) != `{"D":"2025-12-24"}` {
		t.Fatalf("json: %s", raw)
	}
	restore := domain.SetClock(fixed(time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)))
	defer restore()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	if vocab.Today(nil) != vocab.MustDate(2026, 1, 1) || (madrid != nil && vocab.Today(madrid) != vocab.MustDate(2026, 1, 2)) {
		t.Fatal("today must use the domain clock and the requested location")
	}
}

func TestValidPeriod(t *testing.T) {
	jan := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	p, err := vocab.NewValidPeriod(jan, &feb)
	if err != nil {
		t.Fatal(err)
	}
	// Half-open [from, to): consecutive periods never overlap.
	if !p.IsActiveAt(jan) || p.IsActiveAt(feb) || !p.HasExpiredAt(feb) {
		t.Fatal("half-open semantics")
	}
	next, _ := vocab.OpenPeriodFrom(feb)
	if p.Overlaps(next) || !next.IsOpenEnded() {
		t.Fatal("consecutive periods must not overlap")
	}
	if _, err := vocab.NewValidPeriod(feb, &jan); err == nil {
		t.Fatal("end before start accepted")
	}

	restore := domain.SetClock(fixed(time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)))
	defer restore()
	if !p.IsActive() || p.HasExpired() {
		t.Fatal("IsActive must use the domain clock")
	}
	days, _ := vocab.PeriodBetweenDates(vocab.MustDate(2025, 1, 1), vocab.MustDate(2025, 1, 31), nil)
	if !days.Equal(p) {
		t.Fatalf("whole-day period: %v", days)
	}
	raw, _ := json.Marshal(p)
	var back vocab.ValidPeriod
	if err := json.Unmarshal(raw, &back); err != nil || !back.Equal(p) {
		t.Fatalf("json round trip: %s %v", raw, err)
	}
}

func TestTags(t *testing.T) {
	s, err := vocab.NewTagSet("VIP", " vip ", "key  account", "b2b")
	if err != nil || s.String() != "b2b, key account, vip" {
		t.Fatalf("tag set: %s %v", s, err)
	}
	vip, _ := vocab.NewTag("VIP")
	if !s.Contains(vip) || s.Without(vip).Contains(vip) || s.Without(vip).Len() != 2 {
		t.Fatal("set operations")
	}
	empty, err := vocab.NewTagSet()
	if err != nil || empty.Len() != 0 {
		t.Fatal("an empty set is valid")
	}
	_, err = vocab.NewTag("-bad")
	isValidation(t, err)
}

func TestIdentification(t *testing.T) {
	valid := []struct {
		kind   vocab.DocumentKind
		number string
	}{
		{vocab.NationalID, "12345678-z"},
		{vocab.ResidentCard, "X1234567L"},
		{vocab.TaxID, "12345678Z"},
		{vocab.TaxID, "X1234567L"},
		{vocab.TaxID, "A58818501"},
		{vocab.TaxID, "B12345674"},
		{vocab.TaxID, "Q2826000H"},
		{vocab.TaxID, "N0000000J"}, // control 0 -> letter J (C# computed '@')
		{vocab.TaxID, "B00000000"},
		{vocab.SocialSecurity, "281234567840"},
		{vocab.SocialSecurity, "08 01234567 74"},
		{vocab.Passport, "ab123456"}, // normalized before validating (C# rejected lower case)
	}
	for _, c := range valid {
		if _, err := vocab.NewIdentification(c.kind, vocab.Spain, c.number); err != nil {
			t.Errorf("%s %s should be valid: %v", c.kind, c.number, err)
		}
	}
	invalid := []struct {
		kind   vocab.DocumentKind
		number string
	}{
		{vocab.NationalID, "12345678A"},
		{vocab.TaxID, "B12345675"},
		{vocab.TaxID, "Q28260008"}, // Q requires a letter
		{vocab.TaxID, "A5881850A"}, // A requires a digit
		{vocab.SocialSecurity, "281234567841"},
		{vocab.SocialSecurity, "123-45-6789"}, // US format (the C# rule)
	}
	for _, c := range invalid {
		_, err := vocab.NewIdentification(c.kind, vocab.Spain, c.number)
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s %s should be invalid, got %v", c.kind, c.number, err)
		}
	}
	id, _ := vocab.NewIdentification(vocab.TaxID, vocab.Spain, "b-1234567-4")
	if id.Number != "B12345674" || id.String() != "ES/tax_id:B12345674" {
		t.Fatalf("normalization: %s", id)
	}
	if _, err := vocab.NewIdentification(vocab.TaxID, vocab.MustCountryCode("PT"), "123456789"); err != nil {
		t.Fatalf("countries without rules use the generic rule: %v", err)
	}
}

func TestActorAndFactReference(t *testing.T) {
	if !vocab.SystemActor.IsSystem() {
		t.Fatal("system actor")
	}
	a, err := vocab.NewActor(domain.NewUUID(), " Ana ")
	if err != nil || a.IsSystem() || a.Name != "Ana" {
		t.Fatalf("actor: %v %v", a, err)
	}
	_, err = vocab.NewActor(domain.UUID{}, "x")
	isValidation(t, err)

	f, err := vocab.NewFactReference(domain.NewUUID(), " invoice ")
	if err != nil || f.TypeCode != "INVOICE" {
		t.Fatalf("fact: %v %v", f, err)
	}
}

type fixed time.Time

func (f fixed) Now() time.Time { return time.Time(f) }

func TestDescriptionAndRemark(t *testing.T) {
	d, err := vocab.NewDescription("  IVA   general ")
	if err != nil || d.String() != "IVA general" {
		t.Fatalf("description: %q %v", d, err)
	}
	if _, err := vocab.NewDescription("ok"); err != nil {
		t.Fatal("no arbitrary 3-character minimum")
	}
	_, err = vocab.NewDescription(string(make([]rune, 201)))
	isValidation(t, err)
	var r vocab.Remark
	if !r.IsZero() || r.String() != "" {
		t.Fatal("an absent remark has no default text")
	}
}

func TestRegionAndRegulation(t *testing.T) {
	md := vocab.MustRegionCode("es-md")
	if md.String() != "ES-MD" || md.Country() != vocab.Spain {
		t.Fatalf("region: %s", md)
	}
	for _, bad := range []string{"ES", "XX-01", "ES-TOOLONG", "ES-M!"} {
		_, err := vocab.NewRegionCode(bad)
		isValidation(t, err)
	}
	src, err := vocab.NewRegulationSource(vocab.MustName("Ley 37/1992 del IVA"), []vocab.CountryCode{vocab.Spain},
		[]vocab.RegionCode{md}, false, vocab.Description{})
	if err != nil || !src.AppliesIn(vocab.Spain) || !src.AppliesInRegion(md) || src.AppliesInRegion(vocab.MustRegionCode("ES-CT")) {
		t.Fatalf("regulation scope: %+v %v", src, err)
	}
	_, err = vocab.NewRegulationSource(vocab.MustName("Política interna"), nil, nil, true, vocab.Description{})
	isValidation(t, err)
	_, err = vocab.NewRegulationSource(vocab.MustName("x"), []vocab.CountryCode{vocab.MustCountryCode("PT")}, []vocab.RegionCode{md}, false, vocab.Description{})
	isValidation(t, err)
}
