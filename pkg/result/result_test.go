package result_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

func TestResult_Ok(t *testing.T) {
	r := result.Ok(42)

	if !r.IsSuccess() {
		t.Errorf("expected IsSuccess to be true")
	}
	if r.IsFailure() {
		t.Errorf("expected IsFailure to be false")
	}
	if val, err := r.Value(); err != nil || val != 42 {
		t.Errorf("expected value 42 and nil error, got val=%v err=%v", val, err)
	}
	if r.MustValue() != 42 {
		t.Errorf("expected MustValue to return 42")
	}
	if r.ValueOr(100) != 42 {
		t.Errorf("expected ValueOr to return 42")
	}
	if r.Error() != nil {
		t.Errorf("expected nil error, got %v", r.Error())
	}
}

func TestResult_Fail(t *testing.T) {
	testErr := errors.New("boom")
	r := result.Fail[int](testErr)

	if r.IsSuccess() {
		t.Errorf("expected IsSuccess to be false")
	}
	if !r.IsFailure() {
		t.Errorf("expected IsFailure to be true")
	}
	if val, err := r.Value(); err != testErr || val != 0 {
		t.Errorf("expected value 0 and testErr, got val=%v err=%v", val, err)
	}
	if r.ValueOr(100) != 100 {
		t.Errorf("expected ValueOr to return fallback 100")
	}
	if !errors.Is(r.Error(), testErr) {
		t.Errorf("expected error %v, got %v", testErr, r.Error())
	}

	defer func() {
		if recover() == nil {
			t.Errorf("expected MustValue to panic on failure")
		}
	}()
	_ = r.MustValue()
}

func TestResult_FailMsg(t *testing.T) {
	r := result.FailMsg[string]("entity %s not found", "Party-123")
	if !r.IsFailure() {
		t.Fatalf("expected failure")
	}
	if r.Error().Error() != "entity Party-123 not found" {
		t.Errorf("unexpected error message: %s", r.Error().Error())
	}
}

func TestResult_Map(t *testing.T) {
	rOk := result.Ok(10)
	mappedOk := result.Map(rOk, func(n int) string {
		return strconv.Itoa(n * 2)
	})

	if !mappedOk.IsSuccess() || mappedOk.MustValue() != "20" {
		t.Errorf("expected success with '20', got: %v", mappedOk)
	}

	rFail := result.Fail[int](errors.New("err"))
	mappedFail := result.Map(rFail, func(n int) string {
		return strconv.Itoa(n * 2)
	})

	if !mappedFail.IsFailure() {
		t.Errorf("expected mapped failure to remain failure")
	}
}

func TestResult_FlatMap(t *testing.T) {
	rOk := result.Ok("42")
	flatOk := result.FlatMap(rOk, func(s string) result.Result[int] {
		val, err := strconv.Atoi(s)
		if err != nil {
			return result.Fail[int](err)
		}
		return result.Ok(val)
	})

	if !flatOk.IsSuccess() || flatOk.MustValue() != 42 {
		t.Errorf("expected success with 42, got: %v", flatOk)
	}

	rInvalid := result.Ok("not-a-number")
	flatFail := result.FlatMap(rInvalid, func(s string) result.Result[int] {
		val, err := strconv.Atoi(s)
		if err != nil {
			return result.Fail[int](err)
		}
		return result.Ok(val)
	})

	if !flatFail.IsFailure() {
		t.Errorf("expected failure when parsing invalid number")
	}
}
