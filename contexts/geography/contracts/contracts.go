// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Open Host
// Service del contexto de Geografía y datos de referencia. Los límites, los códigos postales y los
// países se referencian por identidad (los mismos GUID que Karpo) o por código ISO.
package contracts

import "context"

// BoundaryRef es lo que otros contextos necesitan para mostrar un límite.
type BoundaryRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}

// MaxBatch es el número máximo de ids por llamada.
const MaxBatch = 900

// Gazetteer resuelve y recorre límites.
type Gazetteer interface {
	// Resolve devuelve los límites que existen entre ids.
	Resolve(ctx context.Context, ids []string) (map[string]BoundaryRef, error)
	// Ancestors devuelve los límites que contienen a id, el más cercano primero (municipio ->
	// provincia -> comunidad autónoma -> país -> subcontinente -> continente).
	Ancestors(ctx context.Context, id string) ([]BoundaryRef, error)
	// Descendants devuelve ids y todos los límites contenidos en ellos (una búsqueda «en Madrid»
	// se convierte en «en cualquiera de estos municipios»).
	Descendants(ctx context.Context, ids []string) ([]string, error)
	// CountryOf devuelve el código ISO 3166 alfa-2 del país que contiene cada límite.
	CountryOf(ctx context.Context, ids []string) (map[string]string, error)
}

// PostalAddressQuery es la parte geográfica de una dirección postal que comprobar.
type PostalAddressQuery struct {
	Country    string // ISO 3166 alfa-2
	PostalCode string
	Boundary   string // opcional: el municipio (o el límite más pequeño) que eligió el usuario
}

// PostalAddressResult es la dirección comprobada: el código normalizado y su entrada de código
// postal.
type PostalAddressResult struct {
	PostalCode   string `json:"postalCode"`
	PostalCodeID string `json:"postalCodeId,omitempty"` // entrada del código en el límite, cuando se conoce
	Boundary     string `json:"boundary,omitempty"`
}

// AddressChecker valida la parte geográfica de las direcciones postales (el
// ContactMechanismApplicationService del C# comprobaba que el código pertenecía al municipio).
// Los errores casan con domain.ErrValidation.
type AddressChecker interface {
	CheckPostalAddress(ctx context.Context, q PostalAddressQuery) (PostalAddressResult, error)
}

// CountryInfo son los datos de referencia que más usan otros contextos.
type CountryInfo struct {
	Alpha2        string   `json:"alpha2"`
	Alpha3        string   `json:"alpha3"`
	Name          string   `json:"name"`
	Boundary      string   `json:"boundary"`
	CallingCode   string   `json:"callingCode,omitempty"`
	DefaultLocale string   `json:"defaultLocale,omitempty"`
	EU            bool     `json:"eu"`
	Eurozone      bool     `json:"eurozone"`
	SEPA          bool     `json:"sepa"`
	Currencies    []string `json:"currencies"` // ISO 4217, la principal primero
	Languages     []string `json:"languages"`  // ISO 639, el predeterminado primero
	TimeZones     []string `json:"timeZones"`  // IANA, el principal primero
}

// Calendar dice los festivos de un lugar: los declarados en el límite y en los que lo contienen
// (el municipio, su provincia, su región, su país). Los fines de semana no son festivos: si
// cuentan lo decide quien pregunta. El día se escribe YYYY-MM-DD.
type Calendar interface {
	IsHoliday(ctx context.Context, boundary, day string) (bool, error)
}

// Reference responde preguntas sobre países.
type Reference interface {
	Country(ctx context.Context, alpha2 string) (CountryInfo, error)
	// CheckIBAN valida un IBAN (los dígitos de control y la longitud y el formato del país).
	CheckIBAN(ctx context.Context, iban string) error
}
