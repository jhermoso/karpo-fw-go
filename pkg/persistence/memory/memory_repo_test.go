package memory_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

type Customer struct {
	ID   string
	Name string
	Age  int
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
	c1 := Customer{ID: "c-1", Name: "Customer 1", Age: 25}
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
	c2 := Customer{ID: "c-2", Name: "Customer 2", Age: 40}
	c3 := Customer{ID: "c-3", Name: "Customer 3", Age: 17}
	_ = repo.Save(ctx, c2)
	_ = repo.Save(ctx, c3)

	resAll := repo.FindAll(ctx)
	if !resAll.IsSuccess() || len(resAll.MustValue()) != 3 {
		t.Fatalf("expected 3 customers, got: %v", resAll.MustValue())
	}

	// FindMatching with Specification
	isAdult := domain.NewSpec(func(c Customer) bool {
		return c.Age >= 18
	})
	resMatching := repo.FindMatching(ctx, isAdult)
	if !resMatching.IsSuccess() || len(resMatching.MustValue()) != 2 {
		t.Fatalf("expected 2 adult customers, got %d", len(resMatching.MustValue()))
	}

	// Count
	resCount := repo.Count(ctx, isAdult)
	if !resCount.IsSuccess() || resCount.MustValue() != 2 {
		t.Fatalf("expected count 2, got: %v", resCount)
	}

	// FindPaged
	pageReq := domain.PageRequest{PageNumber: 1, PageSize: 2}
	resPaged := repo.FindPaged(ctx, isAdult, pageReq)
	if !resPaged.IsSuccess() || len(resPaged.MustValue().Items) != 2 || resPaged.MustValue().TotalCount != 2 {
		t.Fatalf("expected paged result with 2 items and totalCount 2, got %v", resPaged)
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
	err := uow.Do(ctx, func(_ context.Context) error {
		executed = true
		return nil
	})

	if err != nil || !executed {
		t.Fatalf("expected UnitOfWork to execute callback cleanly")
	}
}
