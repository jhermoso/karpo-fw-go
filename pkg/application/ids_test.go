package application_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func parseUUID(s string) (domain.UUID, error) { return domain.ParseUUID(s) }

func codes(t *testing.T, v *domain.Validation) []string {
	t.Helper()
	var ve *domain.ValidationError
	if !errors.As(v.Err(), &ve) {
		return nil
	}
	var out []string
	for _, fe := range ve.Errors {
		out = append(out, fe.Field+"/"+fe.Code)
	}
	return out
}

func uuidN(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1) }

func TestParseIDs_SplitsTrimsAndSkipsEmpty(t *testing.T) {
	var v domain.Validation
	got := application.ParseIDs(&v, []string{" " + uuidN(1) + " ,, " + uuidN(2), "", uuidN(3)}, parseUUID)
	if v.HasErrors() || len(got) != 3 || got[0].String() != uuidN(1) || got[2].String() != uuidN(3) {
		t.Fatalf("got %v, errors %v", got, codes(t, &v))
	}
}

func TestParseIDs_DeduplicatesAfterParsingKeepingFirstOrder(t *testing.T) {
	var v domain.Validation
	upper := "00000000-0000-4000-8000-00000000000A"
	lower := "00000000-0000-4000-8000-00000000000a"
	got := application.ParseIDs(&v, []string{upper, uuidN(5), lower, uuidN(5)}, parseUUID)
	if v.HasErrors() || len(got) != 2 || got[0].String() != lower || got[1].String() != uuidN(5) {
		t.Fatalf("got %v", got)
	}
}

func TestParseIDs_EmptyIsAnEmptyNonNilList(t *testing.T) {
	var v domain.Validation
	got := application.ParseIDs(&v, []string{""}, parseUUID)
	if v.HasErrors() || got == nil || len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestParseIDs_MalformedGivesOneFormatError(t *testing.T) {
	var v domain.Validation
	got := application.ParseIDs(&v, []string{"junk,other," + uuidN(1)}, parseUUID)
	if got != nil || !reflect.DeepEqual(codes(t, &v), []string{"ids/format"}) {
		t.Fatalf("got %v, %v", got, codes(t, &v))
	}
	if msg := v.Err().Error(); msg != "validation failed: ids: invalid id: junk" {
		t.Fatalf("message %q", msg)
	}
}

func TestParseIDs_ZeroIdentityIsAFormatError(t *testing.T) {
	var v domain.Validation
	got := application.ParseIDs(&v, []string{"00000000-0000-0000-0000-000000000000"}, parseUUID)
	if got != nil || !reflect.DeepEqual(codes(t, &v), []string{"ids/format"}) {
		t.Fatalf("got %v, %v", got, codes(t, &v))
	}
}

func many(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuidN(i)
	}
	return out
}

func TestParseIDs_ExactlyTheMaximumPasses(t *testing.T) {
	var v domain.Validation
	got := application.ParseIDs(&v, many(domain.MaxPageSize), parseUUID)
	if v.HasErrors() || len(got) != domain.MaxPageSize {
		t.Fatalf("len %d, %v", len(got), codes(t, &v))
	}
}

func TestParseIDs_OneOverTheMaximumIsARangeError(t *testing.T) {
	var v domain.Validation
	got := application.ParseIDs(&v, many(domain.MaxPageSize+1), parseUUID)
	if got != nil || !reflect.DeepEqual(codes(t, &v), []string{"ids/range"}) {
		t.Fatalf("got %d, %v", len(got), codes(t, &v))
	}
	if msg := v.Err().Error(); msg != "validation failed: ids: at most 1000 ids" {
		t.Fatalf("message %q", msg)
	}
}

func TestParseIDs_DuplicatesDoNotCountTowardsTheMaximum(t *testing.T) {
	var v domain.Validation
	raw := append(many(domain.MaxPageSize), uuidN(0))
	got := application.ParseIDs(&v, raw, parseUUID)
	if v.HasErrors() || len(got) != domain.MaxPageSize {
		t.Fatalf("len %d, %v", len(got), codes(t, &v))
	}
}
