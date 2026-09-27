package domain

// Validator checks a candidate against rules that do not belong to the candidate itself
// (context-dependent or configurable rules; C# IValidator<TEntity>). Aggregates are always
// valid by construction, so validators complement constructors, they do not replace them.
// Return a *ValidationError or a RuleViolationError.
type Validator[T any] interface {
	Validate(candidate T) error
}

// ValidatorFunc adapts a function into a Validator.
type ValidatorFunc[T any] func(candidate T) error

// Validate calls fn.
func (fn ValidatorFunc[T]) Validate(candidate T) error { return fn(candidate) }

// ValidateAll runs every validator and merges their field errors; the first non-validation
// error (e.g. a rule violation) is returned as is.
func ValidateAll[T any](candidate T, validators ...Validator[T]) error {
	var v Validation
	for _, val := range validators {
		err := val.Validate(candidate)
		if err == nil {
			continue
		}
		if _, ok := err.(*ValidationError); !ok {
			return err
		}
		v.Merge("", err)
	}
	return v.Err()
}
