package sqlrepo

import "testing"

// TestRow_Decimal comprueba que Row.Decimal lee exacto lo que entregan los drivers (texto o bytes
// con punto, enteros) y rechaza la coma flotante, que ya habría perdido cifras.
func TestRow_Decimal(t *testing.T) {
	const big = "-1234567890123456789.012345678"
	row := newRow(nil, []string{"s", "b", "i", "n", "f"}, []any{big, []byte(" 2.50 "), int64(-7), nil, 0.1})
	if got := row.Decimal("s").String(); got != big {
		t.Fatalf("string: %s", got)
	}
	if got := row.Decimal("b").String(); got != "2.5" {
		t.Fatalf("bytes: %s", got)
	}
	if got := row.Decimal("i").String(); got != "-7" {
		t.Fatalf("int64: %s", got)
	}
	if !row.Decimal("n").IsZero() || row.NullDecimal("n") != nil || row.NullDecimal("s") == nil {
		t.Fatal("NULL must read as zero / nil")
	}
	if row.Err() != nil {
		t.Fatal(row.Err())
	}
	row.Decimal("f")
	if row.Err() == nil {
		t.Fatal("a float64 was accepted as an exact decimal")
	}
}
