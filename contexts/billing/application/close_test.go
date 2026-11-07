package application

import (
	"context"
	"fmt"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Qué hace el cierre con el error de un cliente: lo anota con su código o corta el cierre.
func TestCloseFailure(t *testing.T) {
	cu := domain.PartyID{UUID: fw.NewUUID()}
	var v fw.Validation
	v.Add("taxCode", "required", "a tax code")
	for err, code := range map[error]string{
		fw.Violation("billing.too_many_lines", "x"): "billing.too_many_lines",
		v.Err(): "billing.close_invalid_line",
		fmt.Errorf("%w: stale version", fw.ErrConflict): "billing.close_conflict",
	} {
		if f, ok := closeFailure(cu, err); !ok || f.Code != code || f.Customer != cu.String() {
			t.Fatalf("%v: %+v %v", err, f, ok)
		}
	}
	if f, _ := closeFailure(cu, fmt.Errorf("%w: stale version 7", fw.ErrConflict)); f.Message == "" || f.Message == "conflict: stale version 7" {
		t.Fatalf("a generic message for a conflict: %q", f.Message)
	}
	for _, err := range []error{context.Canceled, fmt.Errorf("database is down")} {
		if _, ok := closeFailure(cu, err); ok {
			t.Fatalf("%v cuts the close", err)
		}
	}
}
