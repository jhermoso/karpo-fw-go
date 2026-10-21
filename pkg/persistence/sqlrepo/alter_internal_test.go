package sqlrepo

import (
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// fakeDialect es lo mínimo para generar SQL de prueba con un nombre de motor dado.
type fakeDialect struct{ name string }

func (f fakeDialect) Name() string                         { return f.name }
func (fakeDialect) Placeholder(n int) string               { return "?" }
func (fakeDialect) Quote(s string) string                  { return `"` + s + `"` }
func (fakeDialect) LimitOffset(int, int) string            { return "" }
func (fakeDialect) LikeEscape() string                     { return "" }
func (fakeDialect) MaxInList() int                         { return 0 }
func (fakeDialect) BoolValue(b bool) any                   { return b }
func (fakeDialect) UUIDValue(u domain.UUID) any            { return u.String() }
func (fakeDialect) ParseUUID(v any) (domain.UUID, error)   { return ParseUUIDDefault(v) }
func (fakeDialect) TimeValue(t time.Time) any              { return t }
func (fakeDialect) DateValue(int, time.Month, int) any     { return nil }
func (fakeDialect) DateExpr(ph string) string              { return ph }
func (fakeDialect) DecimalExpr(ph string, _, _ int) string { return ph }
func (fakeDialect) IsUniqueViolation(error) bool           { return false }

func TestAlterDecimalSQL(t *testing.T) {
	want := map[string]string{
		"postgres":  `ALTER TABLE "lines" ALTER COLUMN "amount" TYPE NUMERIC(18,2) USING CAST("amount" AS NUMERIC(18,2))`,
		"sqlserver": `ALTER TABLE "lines" ALTER COLUMN "amount" DECIMAL(18,2) NOT NULL`,
		"mysql":     `ALTER TABLE "lines" MODIFY COLUMN "amount" DECIMAL(18,2) NOT NULL`,
	}
	for d, w := range want {
		got := alterDecimalSQL(fakeDialect{d}, "lines", "amount", Dec(18, 2), true)
		if len(got) != 1 || got[0] != w {
			t.Errorf("%s: %v", d, got)
		}
	}
	if got := alterDecimalSQL(fakeDialect{"sqlserver"}, "lines", "amount", Dec(18, 2), false); !strings.HasSuffix(got[0], "DECIMAL(18,2) NULL") {
		t.Errorf("nullable: %v", got)
	}
}

func TestDecimalColumn_Canonical(t *testing.T) {
	c := Dec(6, 2)
	ok := map[string]string{"1": "1", "-1": "-1", "0012.50": "12.5", ".5": "0.5", "-.5": "-0.5", "-0": "0", "9999.99": "9999.99", "0.10": "0.1"}
	for in, want := range ok {
		if got, err := c.canonical(in); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", " 1", "1 ", "+1", "1e3", "NaN", "Infinity", "1.", ".", "-", "1,5", "abc", "10000", "0.123", "--1"} {
		if _, err := c.canonical(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
