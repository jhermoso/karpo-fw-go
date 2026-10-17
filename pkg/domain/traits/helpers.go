package traits

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// equalValue compara valores de instantánea: == para los valores comparables, el método Equal para
// los valores que lo definen (decimales, dinero, periodos) y la forma textual como último recurso.
func equalValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if eq, ok := a.(interface{ Equal(any) bool }); ok {
		return eq.Equal(b)
	}
	if m := reflect.ValueOf(a).MethodByName("Equal"); m.IsValid() && m.Type().NumIn() == 1 &&
		m.Type().NumOut() == 1 && reflect.TypeOf(b).AssignableTo(m.Type().In(0)) {
		return m.Call([]reflect.Value{reflect.ValueOf(b)})[0].Bool()
	}
	if reflect.TypeOf(a).Comparable() && reflect.TypeOf(b).Comparable() {
		return a == b
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func sortChanges(c []FieldChange) {
	slices.SortFunc(c, func(a, b FieldChange) int { return strings.Compare(a.Field, b.Field) })
}

// TestMarked lo implementan los agregados que se pueden marcar como datos de prueba (ITesteable de C#):
// registros creados para demostraciones o comprobaciones en entornos reales, que los informes excluyen.
type TestMarked interface {
	IsTest() bool
}

// TestFlag es la marca incrustable de datos de prueba (el valor cero son datos reales).
type TestFlag struct {
	test bool
}

// RestoredTestFlag reconstruye la marca desde la persistencia.
func RestoredTestFlag(test bool) TestFlag { return TestFlag{test: test} }

// IsTest dice si el agregado son datos de prueba.
func (t TestFlag) IsTest() bool { return t.test }

// MarkAsTest marca el agregado como datos de prueba.
func (t *TestFlag) MarkAsTest() { t.test = true }
