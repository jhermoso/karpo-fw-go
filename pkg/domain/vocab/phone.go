package vocab

import (
	"strings"
)

// Phone is a telephone number in E.164 form ("+34600111222"). The country calling code is
// identified with the ITU-T E.164 assignment table, which is prefix-free, so it is never guessed
// greedily (the C# Telephone read "+34 600 111 222" as country 346).
type Phone struct {
	callingCode string
	national    string
}

// callingCodes are the assigned ITU-T E.164 country calling codes (the set is prefix-free).
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

// NewPhone parses an international number: "+" or "00" followed by the calling code and the
// national number; spaces, dots, dashes and parentheses are ignored.
func NewPhone(s string) (Phone, error) { return parsePhone(s, "") }

// NewPhoneWithDefault parses a number that may omit the international prefix, in which case
// defaultCallingCode (e.g. "34") is assumed.
func NewPhoneWithDefault(s, defaultCallingCode string) (Phone, error) {
	if !phoneCodes[defaultCallingCode] {
		return Phone{}, invalid("phone", "calling_code", "unknown calling code "+quote(defaultCallingCode))
	}
	return parsePhone(s, defaultCallingCode)
}

// MustPhone is like NewPhone but panics on error.
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

// CallingCode returns the country calling code ("34").
func (p Phone) CallingCode() string { return p.callingCode }

// NationalNumber returns the national significant number ("600111222").
func (p Phone) NationalNumber() string { return p.national }

// IsZero reports whether the number is absent.
func (p Phone) IsZero() bool { return p.national == "" }

// String returns the E.164 form ("+34600111222").
func (p Phone) String() string {
	if p.IsZero() {
		return ""
	}
	return "+" + p.callingCode + p.national
}

// Formatted returns "+34 600111222".
func (p Phone) Formatted() string {
	if p.IsZero() {
		return ""
	}
	return "+" + p.callingCode + " " + p.national
}

// MarshalText renders the E.164 form.
func (p Phone) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// UnmarshalText parses an international number.
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
