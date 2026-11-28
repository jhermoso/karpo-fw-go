package main

import (
	"strings"
	"testing"
)

// Códigos reales de la BDDAE del IGN (servicio WFS de unidades administrativas).
func TestIGNCode_RealUnits(t *testing.T) {
	cases := map[[2]string]string{
		{"34040707040", "Municipio"}:          "07040", // Palma
		{"34010404001", "Municipio"}:          "04001", // Abla
		{"34010400000", "Provincia"}:          "04",    // Almería
		{"34040000000", "Comunidad autónoma"}: "ES-IB",
		{"34130000000", "Comunidad autónoma"}: "ES-MD",
	}
	for in, want := range cases {
		if got, ok := IGNCode(in[0], in[1]); !ok || got != want {
			t.Errorf("IGNCode(%v) = %q, %v; quería %s", in, got, ok, want)
		}
	}
	for _, in := range [][2]string{{"34000000000", "País"}, {"3401", "Municipio"}, {"33010404001", "Municipio"}, {"34990000000", "Comunidad autónoma"}} {
		if got, ok := IGNCode(in[0], in[1]); ok {
			t.Errorf("IGNCode(%v) = %q", in, got)
		}
	}
}

func TestCompareUnmatched_IsAClosedList(t *testing.T) {
	expected, err := ReadExpected(strings.NewReader("# comentario\n34040700000 provincia sin nivel\n\n34053500000 provincia sin nivel\n"))
	if err != nil || len(expected) != 2 {
		t.Fatalf("%v %v", expected, err)
	}
	if err := CompareUnmatched(map[string]bool{"34040700000": true, "34053500000": true}, expected); err != nil {
		t.Fatal(err)
	}
	if CompareUnmatched(map[string]bool{"34040700000": true, "34053500000": true, "34999999999": true}, expected) == nil {
		t.Fatal("sobra una clave y no falla")
	}
	if CompareUnmatched(map[string]bool{"34040700000": true}, expected) == nil {
		t.Fatal("falta una clave y no falla")
	}
}

func TestProperties(t *testing.T) {
	p := Properties(Key{Code: "07040", ParentCode: "ES532", Name: "Palma", Level: "local", Kind: "municipality", LAU: "07040"})
	if p["parentCode"] != "ES532" || p["lauCode"] != "07040" || p["hasChildren"] != false || p["level"] != "local" {
		t.Fatal(p)
	}
	if _, ok := Properties(Key{Code: "ES", Kind: "country", Level: "country"})["parentCode"]; ok {
		t.Fatal("un país con parentCode")
	}
}
