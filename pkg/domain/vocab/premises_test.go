package vocab_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func TestPremises(t *testing.T) {
	p, err := vocab.Premises{Number: " 5 ", Qualifier: "bis", Block: "2", Portal: "A", Staircase: "B", Floor: " 3 ", Door: "izquierda"}.Normalized("address")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.String(), "5 bis, bloque 2, portal A, escalera B, planta 3, puerta izquierda"; got != want {
		t.Fatalf("rendering: got %q, want %q", got, want)
	}
	if got := (vocab.Premises{Floor: "3", Door: "B"}).String(); got != "planta 3, puerta B" {
		t.Fatalf("pieces without a number: %q", got)
	}
	if !(vocab.Premises{}).IsZero() || p.IsZero() || (vocab.Premises{}).String() != "" {
		t.Fatal("zero value")
	}

	_, err = vocab.Premises{Floor: "the third one"}.Normalized("address")
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || len(ve.Errors) != 1 || ve.Errors[0].Field != "address.floor" {
		t.Fatalf("a piece of more than ten characters is rejected naming its field: %v", err)
	}
	if _, err := (vocab.Premises{Door: "1234567890"}).Normalized(""); err != nil {
		t.Fatalf("ten characters fit: %v", err)
	}
}
