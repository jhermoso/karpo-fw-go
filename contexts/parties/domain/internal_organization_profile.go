package domain

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // las zonas IANA, también donde el sistema no las trae (Windows, contenedores mínimos)
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// InternalOrganizationProfileKind es el nombre estable del perfil en el registro de auditoría.
const InternalOrganizationProfileKind = "parties.internal-organization-profile"

// ErrInvalidTimeZone es el código de la violación de una zona horaria que no es IANA. Es el código
// de Karpo C# (CompanyErrors.InvalidTimeZone), literal y no con el prefijo parties. de Go, porque
// el contrato de la API es común a los proyectos (docs/DECISIONES-COMUNES.md, D-133).
const ErrInvalidTimeZone = "Company.InvalidTimeZone"

// MaxTimeZoneLength es la longitud de la columna time_zone.
const MaxTimeZoneLength = 64

// Códigos de las violaciones de los ajustes de D-144. Como ErrInvalidTimeZone, sin el prefijo
// parties.: el contrato de la API es común a los tres proyectos.
const (
	ErrInvalidLineRounding       = "Company.InvalidLineRounding"
	ErrInvalidPriceDecimals      = "Company.InvalidPriceDecimals"
	ErrInvalidInstallmentsTiming = "Company.InvalidInstallmentsTiming"
	ErrInvalidCodeKind           = "Company.InvalidCodeKind"
	ErrInvalidCodePattern        = "Company.InvalidCodePattern"
	ErrInvalidDefaultPriceList   = "Company.InvalidDefaultPriceList"
)

// Formas de redondear una línea con descuento (D-128, D-144).
const (
	// LineRoundingOnce redondea una sola vez al final: cantidad × precio − descuento, al céntimo.
	LineRoundingOnce = "once"
	// LineRoundingGrossThenDiscount redondea al céntimo el bruto de la línea y después el neto, ya
	// con el descuento, como la ficha de factura de Trench.
	LineRoundingGrossThenDiscount = "gross_then_discount"
)

// Cuándo nacen los vencimientos de una factura emitida (D-130, D-144).
const (
	// InstallmentsAfterIssue: después de emitir, del evento de la emisión.
	InstallmentsAfterIssue = "after_issue"
	// InstallmentsWithIssue: en la misma transacción que la emisión.
	InstallmentsWithIssue = "with_issue"
)

// MaxPriceDecimals son los decimales con que puede ir, como mucho, el precio de una línea.
const MaxPriceDecimals = 6

// CodeKindInvoiceSeries es el código de una serie de facturación, el primero que admite un patrón
// (D-129, D-144).
const CodeKindInvoiceSeries = "invoice_series"

// CodeKinds son los tipos de código que admiten un patrón.
var CodeKinds = []string{CodeKindInvoiceSeries}

// MaxCodePatternLength es la longitud de la columna pattern.
const MaxCodePatternLength = 200

// InternalOrganizationProfile son los ajustes propios de una empresa interna (la tabla
// internal_organization_profile, como en Karpo C# y en Trench): su zona horaria (D-133) y los
// ajustes de facturación de D-144. Un ajuste vacío (o nil) es el comportamiento de siempre.
type InternalOrganizationProfile struct {
	Organization PartyID
	// TimeZone es un código IANA ("Atlantic/Canary"); vacío, la zona principal del país de la
	// empresa (D-133).
	TimeZone string
	// LineRounding es LineRoundingOnce o LineRoundingGrossThenDiscount; vacío, una sola vez.
	LineRounding string
	// PriceDecimals son los decimales del precio de una línea, de 0 a 6; nil, 4.
	PriceDecimals *int
	// InstallmentsTiming es InstallmentsAfterIssue o InstallmentsWithIssue; vacío, después.
	InstallmentsTiming string
	// DefaultPriceList es la tarifa general de la empresa, una lista de precios de Products (su id,
	// en su forma canónica); vacío, no tiene (D-148). Parties no comprueba que exista: Products se
	// salta una tarifa que no encuentra, que es de otra empresa o que está retirada.
	DefaultPriceList string
	// AutomaticPricing dice si una línea de factura sin pedido toma sola el precio de la cadena de
	// Products (D-148); nil, sí.
	AutomaticPricing *bool
}

// PricesAutomatically dice si la empresa pone precio sola a una línea de factura sin pedido: el
// ajuste, o sí si no lo tiene.
func (p InternalOrganizationProfile) PricesAutomatically() bool {
	return p.AutomaticPricing == nil || *p.AutomaticPricing
}

// SetDefaultPriceList fija la tarifa general: el id de una lista de precios, o vacío para quitarla.
func (p *InternalOrganizationProfile) SetDefaultPriceList(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		p.DefaultPriceList = ""
		return nil
	}
	u, err := fw.ParseUUID(id)
	if err != nil || u.IsZero() {
		return fw.Violation(ErrInvalidDefaultPriceList, "the default price list must be the id of a price list")
	}
	p.DefaultPriceList = u.String()
	return nil
}

// SetAutomaticPricing fija si se pone precio solo a una línea de factura sin pedido; nil lo limpia
// (y vale sí).
func (p *InternalOrganizationProfile) SetAutomaticPricing(on *bool) {
	if on == nil {
		p.AutomaticPricing = nil
		return
	}
	v := *on
	p.AutomaticPricing = &v
}

// Equal dice si dos perfiles guardan lo mismo.
func (p InternalOrganizationProfile) Equal(o InternalOrganizationProfile) bool {
	samePrice := (p.PriceDecimals == nil) == (o.PriceDecimals == nil) && (p.PriceDecimals == nil || *p.PriceDecimals == *o.PriceDecimals)
	sameAuto := (p.AutomaticPricing == nil) == (o.AutomaticPricing == nil) && (p.AutomaticPricing == nil || *p.AutomaticPricing == *o.AutomaticPricing)
	return p.Organization == o.Organization && p.TimeZone == o.TimeZone && p.LineRounding == o.LineRounding && samePrice &&
		p.InstallmentsTiming == o.InstallmentsTiming && p.DefaultPriceList == o.DefaultPriceList && sameAuto
}

// SetLineRounding fija la forma de redondear las líneas: once, gross_then_discount, o vacío para
// limpiarla.
func (p *InternalOrganizationProfile) SetLineRounding(mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != "" && mode != LineRoundingOnce && mode != LineRoundingGrossThenDiscount {
		return fw.Violation(ErrInvalidLineRounding, "the line rounding must be once or gross_then_discount")
	}
	p.LineRounding = mode
	return nil
}

// SetPriceDecimals fija los decimales del precio: de 0 a 6, o nil para limpiarlos.
func (p *InternalOrganizationProfile) SetPriceDecimals(n *int) error {
	if n == nil {
		p.PriceDecimals = nil
		return nil
	}
	if *n < 0 || *n > MaxPriceDecimals {
		return fw.Violation(ErrInvalidPriceDecimals, "the price decimals go from 0 to 6")
	}
	v := *n
	p.PriceDecimals = &v
	return nil
}

// SetInstallmentsTiming fija cuándo nacen los vencimientos: after_issue, with_issue, o vacío para
// limpiarlo.
func (p *InternalOrganizationProfile) SetInstallmentsTiming(timing string) error {
	timing = strings.TrimSpace(timing)
	if timing != "" && timing != InstallmentsAfterIssue && timing != InstallmentsWithIssue {
		return fw.Violation(ErrInvalidInstallmentsTiming, "the installments timing must be after_issue or with_issue")
	}
	p.InstallmentsTiming = timing
	return nil
}

// ParseCodeKind acepta uno de los CodeKinds.
func ParseCodeKind(kind string) (string, error) {
	if !slices.Contains(CodeKinds, kind) {
		return "", fw.Violation(ErrInvalidCodeKind, "the code kind must be one of: "+strings.Join(CodeKinds, ", "))
	}
	return kind, nil
}

// ParseCodePattern acepta un patrón de código: una expresión regular de Go de hasta 200
// caracteres, que un código debe cumplir entero. Go usa RE2, sin retroceso: comprobar un código
// tarda lo que mide el código, sea cual sea el patrón. Devuelve "" para un texto vacío, que quita
// el patrón.
func ParseCodePattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", nil
	}
	if utf8.RuneCountInString(pattern) > MaxCodePatternLength {
		return "", fw.Violation(ErrInvalidCodePattern, "the code pattern has at most 200 characters")
	}
	if _, err := CompileCodePattern(pattern); err != nil {
		return "", fw.Violation(ErrInvalidCodePattern, "the code pattern is not a valid regular expression: "+err.Error())
	}
	return pattern, nil
}

// CompileCodePattern compila un patrón para que un código lo cumpla entero, de principio a fin. El
// patrón se compila antes solo: si no, unos paréntesis desequilibrados se saltarían el anclaje
// («A)|(.*» daría ^(?:A)|(.*)$, que admite cualquier código).
func CompileCodePattern(pattern string) (*regexp.Regexp, error) {
	if _, err := regexp.Compile(pattern); err != nil {
		return nil, err
	}
	return regexp.Compile(`^(?:` + pattern + `)$`)
}

// SetTimeZone fija la zona horaria del perfil: un código IANA que el sistema conoce, o vacío para
// limpiarla.
func (p *InternalOrganizationProfile) SetTimeZone(code string) error {
	tz, err := ParseTimeZone(code)
	if err != nil {
		return err
	}
	p.TimeZone = tz
	return nil
}

// ParseTimeZone acepta un código IANA conocido ("Europe/Madrid", "UTC") y devuelve "" para un
// texto vacío. Un nombre de Windows ("Romance Standard Time") no vale: Trench y Karpo C# guardan
// el mismo dato y lo leen como IANA. Se pide además una barra (o UTC) para no admitir los nombres
// heredados que time.LoadLocation sí conoce ("EST5EDT", "Local").
//
// Que el resultado no dependa del sistema operativo: time.LoadLocation lee antes el directorio de
// zonas del sistema, que en macOS no distingue mayúsculas y que trae además las copias posix/ y
// right/. Por eso se exige la forma canónica de IANA (cada segmento empieza en mayúscula, como
// todos los nombres de la base de datos de zonas: Europe/Madrid, Etc/GMT+3,
// America/Argentina/Buenos_Aires) y se rechazan esos dos prefijos.
func ParseTimeZone(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", nil
	}
	invalid := fw.Violation(ErrInvalidTimeZone, "the time zone must be a known IANA code, such as Europe/Madrid")
	if len(code) > MaxTimeZoneLength || (code != "UTC" && !strings.Contains(code, "/")) ||
		strings.HasPrefix(code, "posix/") || strings.HasPrefix(code, "right/") {
		return "", invalid
	}
	for _, segment := range strings.Split(code, "/") {
		if segment == "" || segment[0] < 'A' || segment[0] > 'Z' {
			return "", invalid
		}
	}
	loc, err := time.LoadLocation(code)
	if err != nil || loc.String() != code {
		return "", invalid
	}
	return code, nil
}

// InternalOrganizationProfiles guarda los perfiles de las empresas internas y sus patrones de
// código (la tabla hija internal_organization_code_pattern).
type InternalOrganizationProfiles interface {
	// Get devuelve el perfil de la organización; si no tiene, uno vacío con Organization puesta.
	Get(ctx context.Context, organization PartyID) (InternalOrganizationProfile, error)
	// Save guarda el perfil, en la unidad de trabajo de ctx.
	Save(ctx context.Context, p InternalOrganizationProfile) error
	// CodePattern devuelve el patrón de un tipo de código de la organización; "" si no tiene.
	CodePattern(ctx context.Context, organization PartyID, kind string) (string, error)
	// SaveCodePattern guarda el patrón, o lo quita si es "", en la unidad de trabajo de ctx.
	SaveCodePattern(ctx context.Context, organization PartyID, kind, pattern string) error
}
