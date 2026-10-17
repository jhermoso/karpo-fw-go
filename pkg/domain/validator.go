package domain

// Validator comprueba un candidato contra reglas que no pertenecen al propio candidato
// (reglas que dependen del contexto o configurables; IValidator<TEntity> de C#). Los agregados son
// siempre válidos por construcción, así que los validadores complementan a los constructores, no los sustituyen.
// Debe devolver un *ValidationError o un RuleViolationError.
type Validator[T any] interface {
	Validate(candidate T) error
}

// ValidatorFunc adapta una función a Validator.
type ValidatorFunc[T any] func(candidate T) error

// Validate llama a fn.
func (fn ValidatorFunc[T]) Validate(candidate T) error { return fn(candidate) }

// ValidateAll ejecuta todos los validadores y junta sus errores de campo; el primer error que no
// es de validación (p. ej. una violación de regla) se devuelve tal cual.
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
