// Package vocab es el lenguaje ubicuo común de Karpo: objetos de valor que todo contexto delimitado
// puede compartir (nombres, datos de contacto, documentos de identificación, fechas y periodos, dinero...).
//
// Cada tipo se reevaluó antes de portarlo desde el framework de C# (véase
// docs/LENGUAJE-UBICUO.md): aquí sólo viven términos genéricos, ajenos al dominio concreto. Los
// términos propios de personas u organizaciones (PersonalName, OrganizationName, Gender, MaritalStatus)
// pertenecen al contexto delimitado Parties.
//
// Convenciones comunes a todos los tipos:
//   - los constructores validan y normalizan; devuelven un *domain.ValidationError con un error de
//     campo por problema, nunca entran en pánico (las variantes Must* existen para constantes y pruebas);
//   - los valores son inmutables y comparables con == salvo que se documente lo contrario (Decimal, Money
//     y TagSet definen Equal/Cmp porque guardan punteros o slices);
//   - el valor cero significa «ausente» y lo indica IsZero;
//   - los tipos de un solo valor implementan encoding.TextMarshaler, así que se serializan como cadenas JSON.
//
// Decimal es github.com/shopspring/decimal: la única dependencia de terceros permitida en el
// dominio (Go no tiene tipo decimal; la coma flotante no es aceptable para importes).
package vocab

import "github.com/jhermoso/karpo-fw-go/pkg/domain"

func invalid(field, code, message string) error {
	var v domain.Validation
	v.Add(field, code, message)
	return v.Err()
}
