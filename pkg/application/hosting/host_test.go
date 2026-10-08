package hosting_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application/hosting"
)

type mod struct {
	name string
	deps []string
	log  *[]string
	fail bool
}

func (m mod) Name() string           { return m.name }
func (m mod) Dependencies() []string { return m.deps }
func (m mod) Start(context.Context) error {
	if m.fail {
		return errors.New("boom")
	}
	*m.log = append(*m.log, "start "+m.name)
	return nil
}
func (m mod) Stop(context.Context) error { *m.log = append(*m.log, "stop "+m.name); return nil }

func TestHost_OrdersModulesByDependencies(t *testing.T) {
	var log []string
	host, err := hosting.NewHost(
		mod{name: "ErpDetail", deps: []string{"ErpKernel"}, log: &log},
		mod{name: "Fw", log: &log},
		mod{name: "ErpKernel", deps: []string{"Fw"}, log: &log},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"start Fw", "start ErpKernel", "start ErpDetail", "stop ErpDetail", "stop ErpKernel", "stop Fw"}
	if !slices.Equal(log, want) {
		t.Fatalf("want %v got %v", want, log)
	}

	if _, err := hosting.NewHost(mod{name: "A", deps: []string{"B"}}, mod{name: "B", deps: []string{"A"}}); err == nil {
		t.Fatal("expected cycle error")
	}
	if _, err := hosting.NewHost(mod{name: "A", deps: []string{"Missing"}}); err == nil {
		t.Fatal("expected unknown dependency error")
	}

	log = nil
	host, _ = hosting.NewHost(mod{name: "Fw", log: &log}, mod{name: "Bad", deps: []string{"Fw"}, fail: true, log: &log})
	if err := host.Start(context.Background()); err == nil {
		t.Fatal("expected start failure")
	}
	if !slices.Equal(log, []string{"start Fw", "stop Fw"}) {
		t.Fatalf("started modules must be stopped on failure: %v", log)
	}
}
