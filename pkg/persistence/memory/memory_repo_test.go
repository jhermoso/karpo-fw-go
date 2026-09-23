package memory_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

type Customer struct {
	ID   string
	Name string
}

func TestMemoryRepository(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository[string, Customer](func(c Customer) string {
		return c.ID
	})

	// Find non-existent
	resNotFound := repo.FindByID(ctx, "c-1")
	if !resNotFound.IsFailure() {
		t.Fatalf("expected failure when finding non-existent entity")
	}

	// Save
	c1 := Customer{ID: "c-1", Name: "Customer 1"}
	resSave := repo.Save(ctx, c1)
	if !resSave.IsSuccess() || resSave.MustValue().Name != "Customer 1" {
		t.Fatalf("failed to save customer")
	}

	// FindByID
	resFind := repo.FindByID(ctx, "c-1")
	if !resFind.IsSuccess() || resFind.MustValue().Name != "Customer 1" {
		t.Fatalf("expected to find customer 1")
	}

	// FindAll
	c2 := Customer{ID: "c-2", Name: "Customer 2"}
	_ = repo.Save(ctx, c2)
	resAll := repo.FindAll(ctx)
	if !resAll.IsSuccess() || len(resAll.MustValue()) != 2 {
		t.Fatalf("expected 2 customers, got: %v", resAll.MustValue())
	}

	// Delete
	resDel := repo.Delete(ctx, "c-1")
	if !resDel.IsSuccess() {
		t.Fatalf("expected delete to succeed")
	}

	resAfterDel := repo.FindByID(ctx, "c-1")
	if !resAfterDel.IsFailure() {
		t.Fatalf("expected deleted entity not to be found")
	}
}

func TestMemoryUnitOfWork(t *testing.T) {
	uow := &memory.MemoryUnitOfWork{}
	ctx := context.Background()

	var executed bool
	err := uow.Do(ctx, func(ctx context.Context) error {
		executed = true
		return nil
	})

	if err != nil || !executed {
		t.Fatalf("expected UnitOfWork to execute callback cleanly")
	}
}
