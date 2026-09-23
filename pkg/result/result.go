// Package result provides a functional, type-safe representation of success or failure.
// It avoids runtime exceptions and provides compile-time safety for fallible operations.
package result

import (
	"errors"
	"fmt"
)

// Result represents the outcome of an operation that may succeed with a value of type T or fail with an error.
type Result[T any] struct {
	value T
	err   error
	isOk  bool
}

// Ok creates a successful Result containing value.
func Ok[T any](value T) Result[T] {
	return Result[T]{
		value: value,
		err:   nil,
		isOk:  true,
	}
}

// Fail creates a failed Result containing err. If err is nil, a default error is used.
func Fail[T any](err error) Result[T] {
	if err == nil {
		err = errors.New("unspecified error")
	}
	var zero T
	return Result[T]{
		value: zero,
		err:   err,
		isOk:  false,
	}
}

// FailMsg creates a failed Result with a formatted error message.
func FailMsg[T any](format string, args ...any) Result[T] {
	return Fail[T](fmt.Errorf(format, args...))
}

// IsSuccess returns true if the operation succeeded.
func (r Result[T]) IsSuccess() bool {
	return r.isOk
}

// IsFailure returns true if the operation failed.
func (r Result[T]) IsFailure() bool {
	return !r.isOk
}

// Value returns the inner value and error.
func (r Result[T]) Value() (T, error) {
	return r.value, r.err
}

// MustValue returns the inner value if successful, or panics if failed.
func (r Result[T]) MustValue() T {
	if !r.isOk {
		panic(fmt.Sprintf("called MustValue on failed Result: %v", r.err))
	}
	return r.value
}

// ValueOr returns the inner value if successful, or the provided fallback if failed.
func (r Result[T]) ValueOr(fallback T) T {
	if !r.isOk {
		return fallback
	}
	return r.value
}

// Error returns the error if failed, or nil if successful.
func (r Result[T]) Error() error {
	return r.err
}

// Map transforms a Result[T] into a Result[U] using the provided mapping function if successful.
func Map[T, U any](r Result[T], fn func(T) U) Result[U] {
	if r.IsFailure() {
		return Fail[U](r.err)
	}
	return Ok(fn(r.value))
}

// FlatMap chains operations that return Result[U].
func FlatMap[T, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.IsFailure() {
		return Fail[U](r.err)
	}
	return fn(r.value)
}
