package vocab

import "strings"

// LanguageCode es un código de idioma ISO 639-1 de dos letras ("es", "en", "ca"...).
type LanguageCode struct{ code string }

const isoLanguages = "aa ab ae af ak am an ar as av ay az ba be bg bh bi bm bn bo br bs ca ce ch co cr cs cu cv cy da de dv dz " +
	"ee el en eo es et eu fa ff fi fj fo fr fy ga gd gl gn gu gv ha he hi ho hr ht hu hy hz ia id ie ig ii ik io is it iu ja jv " +
	"ka kg ki kj kk kl km kn ko kr ks ku kv kw ky la lb lg li ln lo lt lu lv mg mh mi mk ml mn mr ms mt my na nb nd ne ng nl nn " +
	"no nr nv ny oc oj om or os pa pi pl ps pt qu rm rn ro ru rw sa sc sd se sg si sk sl sm sn so sq sr ss st su sv sw ta te tg " +
	"th ti tk tl tn to tr ts tt tw ty ug uk ur uz ve vi vo wa wo xh yi yo za zh zu"

var languages = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range strings.Fields(isoLanguages) {
		m[c] = true
	}
	return m
}()

// NewLanguageCode valida un código ISO 639-1 (sin distinguir mayúsculas); se guarda en minúsculas.
func NewLanguageCode(s string) (LanguageCode, error) {
	c := strings.ToLower(strings.TrimSpace(s))
	if !languages[c] {
		return LanguageCode{}, invalid("language", "iso639", "unknown ISO 639-1 language "+quote(s))
	}
	return LanguageCode{c}, nil
}

// String devuelve el código.
func (c LanguageCode) String() string { return c.code }

// IsZero dice si falta el código.
func (c LanguageCode) IsZero() bool { return c.code == "" }
