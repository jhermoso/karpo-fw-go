package vocab

import (
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// FactReference points to a business fact issued by another bounded context (an invoice, a
// delivery note...): the fact identity plus its type code. It is the shared-kernel link used by
// accounting and documents across ErpKernel, ErpDetail and the sector kernels.
type FactReference struct {
	FactID   domain.UUID `json:"factId"`
	TypeCode string      `json:"factTypeCode"`
}

// NewFactReference validates the reference; the type code is normalized to upper case.
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

// IsZero reports whether the reference is absent.
func (f FactReference) IsZero() bool { return f.FactID.IsZero() }

// String returns "TYPE:id".
func (f FactReference) String() string { return f.TypeCode + ":" + f.FactID.String() }
