package vocab

import "strings"

// CountryCode is an officially assigned ISO 3166-1 alpha-2 country code ("ES", "PT"...).
type CountryCode struct{ code string }

// Spain is the default country of many Karpo rules.
var Spain = CountryCode{"ES"}

const isoCountries = "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS " +
	"BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET " +
	"FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO " +
	"IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH " +
	"MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM " +
	"PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG " +
	"TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW"

var countries = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range strings.Fields(isoCountries) {
		m[c] = true
	}
	return m
}()

// NewCountryCode validates an ISO 3166-1 alpha-2 code (case-insensitive).
func NewCountryCode(s string) (CountryCode, error) {
	c := strings.ToUpper(strings.TrimSpace(s))
	if !countries[c] {
		return CountryCode{}, invalid("country", "iso3166", "unknown ISO 3166-1 country "+quote(s))
	}
	return CountryCode{c}, nil
}

// MustCountryCode is like NewCountryCode but panics on error.
func MustCountryCode(s string) CountryCode {
	c, err := NewCountryCode(s)
	if err != nil {
		panic(err)
	}
	return c
}

// String returns the code.
func (c CountryCode) String() string { return c.code }

// IsZero reports whether the code is absent.
func (c CountryCode) IsZero() bool { return c.code == "" }

// MarshalText implements encoding.TextMarshaler.
func (c CountryCode) MarshalText() ([]byte, error) { return []byte(c.code), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (c *CountryCode) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*c = CountryCode{}
		return nil
	}
	v, err := NewCountryCode(string(b))
	if err == nil {
		*c = v
	}
	return err
}
