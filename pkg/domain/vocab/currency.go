package vocab

import "strings"

// CurrencyCode es un código alfabético de moneda ISO 4217 en vigor ("EUR", "USD"...).
type CurrencyCode struct{ code string }

// Monedas habituales.
var (
	EUR = CurrencyCode{"EUR"}
	USD = CurrencyCode{"USD"}
	GBP = CurrencyCode{"GBP"}
)

// isoCurrencies enumera los códigos ISO 4217 en vigor (sin fondos ni metales preciosos).
const isoCurrencies = "AED AFN ALL AMD ANG AOA ARS AUD AWG AZN BAM BBD BDT BGN BHD BIF BMD BND BOB BRL BSD BTN BWP BYN BZD " +
	"CAD CDF CHF CLF CLP CNY COP CRC CUP CVE CZK DJF DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ " +
	"GYD HKD HNL HTG HUF IDR ILS INR IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL " +
	"LYD MAD MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MYR MZN NAD NGN NIO NOK NPR NZD OMR PAB PEN PGK PHP PKR PLN " +
	"PYG QAR RON RSD RUB RWF SAR SBD SCR SDG SEK SGD SHP SLE SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND TOP TRY TTD " +
	"TWD TZS UAH UGX USD UYU UYW UZS VED VES VND VUV WST XAF XCD XCG XOF XPF YER ZAR ZMW ZWG"

// minorUnits enumera las monedas cuya unidad menor no es de 2 decimales.
var minorUnits = map[string]int32{
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	"CLF": 4, "UYW": 4,
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0,
	"RWF": 0, "UGX": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
}

var currencies = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range strings.Fields(isoCurrencies) {
		m[c] = true
	}
	return m
}()

// NewCurrencyCode valida un código ISO 4217 (sin distinguir mayúsculas).
func NewCurrencyCode(s string) (CurrencyCode, error) {
	c := strings.ToUpper(strings.TrimSpace(s))
	if !currencies[c] {
		return CurrencyCode{}, invalid("currency", "iso4217", "unknown ISO 4217 currency "+quote(s))
	}
	return CurrencyCode{c}, nil
}

// MustCurrencyCode es como NewCurrencyCode pero entra en pánico si hay error.
func MustCurrencyCode(s string) CurrencyCode {
	c, err := NewCurrencyCode(s)
	if err != nil {
		panic(err)
	}
	return c
}

// String devuelve el código.
func (c CurrencyCode) String() string { return c.code }

// IsZero dice si falta el código.
func (c CurrencyCode) IsZero() bool { return c.code == "" }

// MinorUnits devuelve el número de decimales de la moneda (2 para EUR, 0 para JPY...).
func (c CurrencyCode) MinorUnits() int32 {
	if n, ok := minorUnits[c.code]; ok {
		return n
	}
	return 2
}

// MarshalText implementa encoding.TextMarshaler.
func (c CurrencyCode) MarshalText() ([]byte, error) { return []byte(c.code), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (c *CurrencyCode) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*c = CurrencyCode{}
		return nil
	}
	v, err := NewCurrencyCode(string(b))
	if err == nil {
		*c = v
	}
	return err
}
