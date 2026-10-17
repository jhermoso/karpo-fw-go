package vocab

import (
	"strings"
)

// Phone es un número de teléfono en forma E.164 ("+34600111222"). El prefijo telefónico de país
// se identifica con la tabla de asignaciones E.164 de la UIT-T, que está libre de prefijos, así que
// nunca se adivina de forma voraz (el Telephone de C# leía "+34 600 111 222" como país 346).
type Phone struct {
	callingCode string
	national    string
}

// callingCodes son los prefijos telefónicos de país asignados por la UIT-T E.164 (el conjunto está libre de prefijos).
const callingCodes = "1 7 20 27 30 31 32 33 34 36 39 40 41 43 44 45 46 47 48 49 51 52 53 54 55 56 57 58 60 61 62 63 64 65 " +
	"66 81 82 84 86 90 91 92 93 94 95 98 211 212 213 216 218 220 221 222 223 224 225 226 227 228 229 230 231 232 " +
	"233 234 235 236 237 238 239 240 241 242 243 244 245 246 247 248 249 250 251 252 253 254 255 256 257 258 260 " +
	"261 262 263 264 265 266 267 268 269 290 291 297 298 299 350 351 352 353 354 355 356 357 358 359 370 371 372 " +
	"373 374 375 376 377 378 379 380 381 382 383 385 386 387 389 420 421 423 500 501 502 503 504 505 506 507 508 " +
	"509 590 591 592 593 594 595 596 597 598 599 670 672 673 674 675 676 677 678 679 680 681 682 683 685 686 687 " +
	"688 689 690 691 692 800 808 850 852 853 855 856 870 878 880 881 882 883 886 888 960 961 962 963 964 965 966 " +
	"967 968 970 971 972 973 974 975 976 977 979 992 993 994 995 996 998"

var phoneCodes = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range strings.Fields(callingCodes) {
		m[c] = true
	}
	return m
}()

// NewPhone analiza un número internacional: "+" o "00" seguido del prefijo de país y del número
// nacional; los espacios, puntos, guiones y paréntesis se ignoran.
func NewPhone(s string) (Phone, error) { return parsePhone(s, "") }

// NewPhoneWithDefault analiza un número que puede omitir el prefijo internacional, en cuyo caso
// se supone defaultCallingCode (p. ej. "34").
func NewPhoneWithDefault(s, defaultCallingCode string) (Phone, error) {
	if !phoneCodes[defaultCallingCode] {
		return Phone{}, invalid("phone", "calling_code", "unknown calling code "+quote(defaultCallingCode))
	}
	return parsePhone(s, defaultCallingCode)
}

// MustPhone es como NewPhone pero entra en pánico si hay error.
func MustPhone(s string) Phone {
	p, err := NewPhone(s)
	if err != nil {
		panic(err)
	}
	return p
}

func parsePhone(raw, def string) (Phone, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Phone{}, invalid("phone", "required", "phone number is required")
	}
	international := false
	switch {
	case strings.HasPrefix(s, "+"):
		international, s = true, s[1:]
	case strings.HasPrefix(s, "00"):
		international, s = true, s[2:]
	}
	var digits strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '.' || r == '-' || r == '(' || r == ')':
		default:
			return Phone{}, invalid("phone", "format", "invalid character in phone number "+quote(raw))
		}
	}
	d := digits.String()

	var cc string
	if international {
		for n := 1; n <= 3 && n <= len(d); n++ {
			if phoneCodes[d[:n]] {
				cc = d[:n]
				break
			}
		}
		if cc == "" {
			return Phone{}, invalid("phone", "calling_code", "unknown country calling code in "+quote(raw))
		}
		d = d[len(cc):]
	} else {
		if def == "" {
			return Phone{}, invalid("phone", "format", "international prefix (+ or 00) required in "+quote(raw))
		}
		cc = def
	}
	if len(d) < 4 || len(cc)+len(d) > 15 {
		return Phone{}, invalid("phone", "length", "invalid phone number length in "+quote(raw))
	}
	return Phone{callingCode: cc, national: d}, nil
}

// CallingCode devuelve el prefijo telefónico de país ("34").
func (p Phone) CallingCode() string { return p.callingCode }

// NationalNumber devuelve el número nacional significativo ("600111222").
func (p Phone) NationalNumber() string { return p.national }

// IsZero dice si falta el número.
func (p Phone) IsZero() bool { return p.national == "" }

// String devuelve la forma E.164 ("+34600111222").
func (p Phone) String() string {
	if p.IsZero() {
		return ""
	}
	return "+" + p.callingCode + p.national
}

// Formatted devuelve "+34 600111222".
func (p Phone) Formatted() string {
	if p.IsZero() {
		return ""
	}
	return "+" + p.callingCode + " " + p.national
}

// MarshalText escribe la forma E.164.
func (p Phone) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// UnmarshalText analiza un número internacional.
func (p *Phone) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*p = Phone{}
		return nil
	}
	v, err := NewPhone(string(b))
	if err == nil {
		*p = v
	}
	return err
}
