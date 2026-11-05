package domain

import (
	"context"
	"strings"
	"time"
	_ "time/tzdata" // las zonas IANA, también donde el sistema no las trae (Windows, contenedores mínimos)

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

// InternalOrganizationProfile son los ajustes propios de una empresa interna (la tabla
// internal_organization_profile, como en Karpo C# y en Trench). Hoy, sólo su zona horaria.
type InternalOrganizationProfile struct {
	Organization PartyID
	// TimeZone es un código IANA ("Atlantic/Canary"); vacío, la zona principal del país de la
	// empresa (D-133).
	TimeZone string
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

// InternalOrganizationProfiles guarda los perfiles de las empresas internas.
type InternalOrganizationProfiles interface {
	// Get devuelve el perfil de la organización; si no tiene, uno vacío con Organization puesta.
	Get(ctx context.Context, organization PartyID) (InternalOrganizationProfile, error)
	// Save guarda el perfil, en la unidad de trabajo de ctx.
	Save(ctx context.Context, p InternalOrganizationProfile) error
}
