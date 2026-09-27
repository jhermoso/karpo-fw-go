package traits

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// equalValue compares snapshot values: == for comparable values, Equal methods for values that
// define it (decimals, money, periods), and the textual form as a last resort.
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

// TestMarked is implemented by aggregates that can be flagged as test data (C# ITesteable):
// records created for demos or verification in real environments, which reports exclude.
type TestMarked interface {
	IsTest() bool
}

// TestFlag is the embeddable test-data flag (the zero value is real data).
type TestFlag struct {
	test bool
}

// RestoredTestFlag rebuilds the flag from persistence.
func RestoredTestFlag(test bool) TestFlag { return TestFlag{test: test} }

// IsTest reports whether the aggregate is test data.
func (t TestFlag) IsTest() bool { return t.test }

// MarkAsTest flags the aggregate as test data.
func (t *TestFlag) MarkAsTest() { t.test = true }
