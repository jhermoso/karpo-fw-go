package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// La zona de la empresa es un código IANA conocido; un nombre de Windows no vale, y vacío la
// limpia (D-133).
func TestInternalOrganizationProfile_TimeZone(t *testing.T) {
	p := domain.InternalOrganizationProfile{Organization: domain.NewPartyID()}
	for _, code := range []string{"Atlantic/Canary", " Europe/Madrid ", "America/Argentina/Buenos_Aires", "Etc/GMT+3", "America/Port-au-Prince", "UTC"} {
		if err := p.SetTimeZone(code); err != nil || p.TimeZone != strings.TrimSpace(code) {
			t.Fatalf("%q: %q %v", code, p.TimeZone, err)
		}
	}
	for _, code := range []string{"Romance Standard Time", "W. Europe Standard Time", "Mars/Olympus", "EST5EDT", "Local",
		"Europe/../Madrid", strings.Repeat("A/", 40), "europe/madrid", "Europe/madrid", "utc", "posix/Europe/Madrid",
		"right/Europe/Madrid", "/Europe/Madrid", "Europe//Madrid"} {
		err := p.SetTimeZone(code)
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != domain.ErrInvalidTimeZone || p.TimeZone != "UTC" {
			t.Fatalf("%q must be refused, keeping the zone: %v %q", code, err, p.TimeZone)
		}
	}
	for _, code := range []string{"", "   "} {
		p.TimeZone = "UTC"
		if err := p.SetTimeZone(code); err != nil || p.TimeZone != "" {
			t.Fatalf("%q clears the zone: %q %v", code, p.TimeZone, err)
		}
	}
}

// Los ajustes de D-144: los enumerados aceptan sus valores y vacío (lo de siempre); lo demás es un
// 422 con su código, sin tocar el valor que había.
func TestInternalOrganizationProfile_Settings(t *testing.T) {
	violation := func(err error, code string) bool {
		var rv *fw.RuleViolationError
		return errors.As(err, &rv) && rv.Code == code
	}
	p := domain.InternalOrganizationProfile{Organization: domain.NewPartyID()}
	for _, mode := range []string{"once", "gross_then_discount", " once ", ""} {
		if err := p.SetLineRounding(mode); err != nil || p.LineRounding != strings.TrimSpace(mode) {
			t.Fatalf("rounding %q: %q %v", mode, p.LineRounding, err)
		}
	}
	p.LineRounding = "once"
	for _, mode := range []string{"Once", "NetOnce", "gross", "half_even"} {
		if err := p.SetLineRounding(mode); !violation(err, domain.ErrInvalidLineRounding) || p.LineRounding != "once" {
			t.Fatalf("rounding %q must be refused: %v %q", mode, err, p.LineRounding)
		}
	}
	for _, timing := range []string{"after_issue", "with_issue", ""} {
		if err := p.SetInstallmentsTiming(timing); err != nil || p.InstallmentsTiming != timing {
			t.Fatalf("timing %q: %q %v", timing, p.InstallmentsTiming, err)
		}
	}
	for _, timing := range []string{"InIssueTransaction", "later"} {
		if err := p.SetInstallmentsTiming(timing); !violation(err, domain.ErrInvalidInstallmentsTiming) {
			t.Fatalf("timing %q must be refused: %v", timing, err)
		}
	}
	for n := 0; n <= 6; n++ {
		if err := p.SetPriceDecimals(&n); err != nil || p.PriceDecimals == nil || *p.PriceDecimals != n {
			t.Fatalf("%d decimals: %v %v", n, p.PriceDecimals, err)
		}
	}
	n := 2
	_ = p.SetPriceDecimals(&n)
	n = 3 // el perfil guarda su copia
	if *p.PriceDecimals != 2 {
		t.Fatal("the profile keeps its own copy of the decimals")
	}
	for _, bad := range []int{-1, 7, 18} {
		if err := p.SetPriceDecimals(&bad); !violation(err, domain.ErrInvalidPriceDecimals) || *p.PriceDecimals != 2 {
			t.Fatalf("%d decimals must be refused: %v", bad, err)
		}
	}
	if err := p.SetPriceDecimals(nil); err != nil || p.PriceDecimals != nil {
		t.Fatal("nil clears the decimals")
	}
	two, other := 2, 2
	a := domain.InternalOrganizationProfile{Organization: p.Organization, PriceDecimals: &two}
	b := domain.InternalOrganizationProfile{Organization: p.Organization, PriceDecimals: &other}
	if !a.Equal(b) || a.Equal(domain.InternalOrganizationProfile{Organization: p.Organization}) {
		t.Fatal("profiles compare their values, not their pointers")
	}
}

// El patrón de un código: una expresión regular de Go de hasta 200 caracteres, que el código cumple
// entero; sólo para los tipos de código conocidos.
// La tarifa general es el id de una lista de precios, o vacío; el precio automático, nil es sí
// (D-148).
func TestInternalOrganizationProfile_PricingSettings(t *testing.T) {
	p := domain.InternalOrganizationProfile{Organization: domain.NewPartyID()}
	id := fw.NewUUID()
	if err := p.SetDefaultPriceList(" " + strings.ToUpper(id.String()) + " "); err != nil || p.DefaultPriceList != id.String() {
		t.Fatalf("default price list: %q %v", p.DefaultPriceList, err)
	}
	for _, bad := range []string{"mayor", "00000000-0000-0000-0000-000000000000"} {
		var rv *fw.RuleViolationError
		if err := p.SetDefaultPriceList(bad); !errors.As(err, &rv) || rv.Code != domain.ErrInvalidDefaultPriceList || p.DefaultPriceList != id.String() {
			t.Fatalf("%q must be refused: %v", bad, err)
		}
	}
	if err := p.SetDefaultPriceList(""); err != nil || p.DefaultPriceList != "" {
		t.Fatalf("cleared: %q %v", p.DefaultPriceList, err)
	}
	if !p.PricesAutomatically() {
		t.Fatal("automatic pricing by default")
	}
	off := false
	p.SetAutomaticPricing(&off)
	off = true // el perfil guarda su copia
	if p.PricesAutomatically() || p.Equal(domain.InternalOrganizationProfile{Organization: p.Organization}) {
		t.Fatalf("automatic pricing off: %+v", p)
	}
	p.SetAutomaticPricing(nil)
	if !p.PricesAutomatically() || !p.Equal(domain.InternalOrganizationProfile{Organization: p.Organization}) {
		t.Fatalf("automatic pricing cleared: %+v", p)
	}
}

func TestCodePattern(t *testing.T) {
	violation := func(err error, code string) bool {
		var rv *fw.RuleViolationError
		return errors.As(err, &rv) && rv.Code == code
	}
	if k, err := domain.ParseCodeKind("invoice_series"); err != nil || k != domain.CodeKindInvoiceSeries {
		t.Fatal(k, err)
	}
	for _, kind := range []string{"", "InvoiceSeries", "customer_number"} {
		if _, err := domain.ParseCodeKind(kind); !violation(err, domain.ErrInvalidCodeKind) {
			t.Fatalf("kind %q: %v", kind, err)
		}
	}
	for _, pattern := range []string{"[A-Z]{2}", "F[A-Z0-9_./-]{0,19}", " FA|FR ", strings.Repeat("A", 200)} {
		got, err := domain.ParseCodePattern(pattern)
		if err != nil || got != strings.TrimSpace(pattern) {
			t.Fatalf("pattern %q: %q %v", pattern, got, err)
		}
	}
	if got, err := domain.ParseCodePattern("   "); err != nil || got != "" {
		t.Fatal("a blank pattern clears it")
	}
	// (a+)+$ es el clásico de retroceso catastrófico: RE2 lo acepta y lo comprueba en tiempo lineal.
	for _, pattern := range []string{"[A-Z", "(?P<x", "a**", `\`, strings.Repeat("A", 201), "x{1001}"} {
		if _, err := domain.ParseCodePattern(pattern); !violation(err, domain.ErrInvalidCodePattern) {
			t.Fatalf("pattern %q must be refused: %v", pattern, err)
		}
	}
	if _, err := domain.ParseCodePattern(`A)|(.*`); !violation(err, domain.ErrInvalidCodePattern) {
		t.Fatalf("unbalanced parentheses do not skip the anchors: %v", err)
	}
	if _, err := domain.CompileCodePattern(`A)|(.*`); err == nil {
		t.Fatal("A)|(.* does not compile on its own")
	}
	re, err := domain.CompileCodePattern("FA|FR")
	if err != nil || !re.MatchString("FA") || re.MatchString("FAX") || re.MatchString("XFR") {
		t.Fatal("the code matches the whole pattern, not a part of it")
	}
	evil, _ := domain.CompileCodePattern("(a+)+$")
	if evil.MatchString(strings.Repeat("a", 5000) + "!") {
		t.Fatal("no match, and quickly")
	}
}
