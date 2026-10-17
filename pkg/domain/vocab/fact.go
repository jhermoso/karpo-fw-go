package vocab

import (
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// FactReference apunta a un hecho de negocio emitido por otro contexto delimitado (una factura, un
// albarán...): la identidad del hecho más su código de tipo. Es el enlace del núcleo compartido que
// usan la contabilidad y los documentos entre ErpKernel, ErpDetail y los núcleos sectoriales.
type FactReference struct {
	FactID   domain.UUID `json:"factId"`
	TypeCode string      `json:"factTypeCode"`
}

// NewFactReference valida la referencia; el código de tipo se normaliza a mayúsculas.
func NewFactReference(factID domain.UUID, typeCode string) (FactReference, error) {
	code := strings.ToUpper(strings.TrimSpace(typeCode))
	var v domain.Validation
	v.Require(!factID.IsZero(), "factId", "required", "fact id is required")
	v.Require(code != "", "factTypeCode", "required", "fact type code is required")
	v.Require(len(code) <= 50, "factTypeCode", "length", "fact type code must have at most 50 characters")
	if err := v.Err(); err != nil {
		return FactReference{}, err
	}
	return FactReference{FactID: factID, TypeCode: code}, nil
}

// IsZero dice si falta la referencia.
func (f FactReference) IsZero() bool { return f.FactID.IsZero() }

// String devuelve "TYPE:id".
func (f FactReference) String() string { return f.TypeCode + ":" + f.FactID.String() }
