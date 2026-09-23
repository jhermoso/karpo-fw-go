package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// 1. Value Object Example: Money
type Money struct {
	Amount   float64
	Currency string
}

func (m Money) Equals(other Money) bool {
	return m.Amount == other.Amount && m.Currency == other.Currency
}

// 2. Entity Example: Customer
type Customer struct {
	domain.BaseEntity[string]
	Name string
}

// 3. Aggregate Root Example: Account
type Account struct {
	domain.BaseAggregateRoot[string]
	Balance Money
}

func NewAccount(id string, initial Money) *Account {
	acc := &Account{
		BaseAggregateRoot: domain.NewBaseAggregateRoot(id),
		Balance:           initial,
	}
	acc.AddDomainEvent(events.BaseEvent{
		EventID:        "evt-acc-created",
		EventType:      "account.created",
		EventTimestamp: time.Now(),
		EventPayload:   id,
	})
	return acc
}

// 4. Domain Service Example: TransferService
type TransferService struct {
	domain.BaseDomainService
}

func (s *TransferService) Transfer(from, to *Account, amount Money) result.Result[bool] {
	if from.Balance.Amount < amount.Amount {
		return result.FailMsg[bool]("insufficient funds")
	}
	from.Balance.Amount -= amount.Amount
	to.Balance.Amount += amount.Amount
	return result.Ok(true)
}

func TestValueObject_Equality(t *testing.T) {
	m1 := Money{Amount: 100.50, Currency: "EUR"}
	m2 := Money{Amount: 100.50, Currency: "EUR"}
	m3 := Money{Amount: 100.50, Currency: "USD"}

	if !m1.Equals(m2) {
		t.Fatalf("expected identical value objects to be equal")
	}
	if m1.Equals(m3) {
		t.Fatalf("expected value objects with different currency not to be equal")
	}
}

func TestEntity_Equality(t *testing.T) {
	c1 := Customer{BaseEntity: domain.NewBaseEntity("cust-1"), Name: "Customer One"}
	c2 := Customer{BaseEntity: domain.NewBaseEntity("cust-1"), Name: "Customer Changed Name"}
	c3 := Customer{BaseEntity: domain.NewBaseEntity("cust-2"), Name: "Customer One"}

	// Same ID -> Equal (even with different attributes)
	if !c1.Equals(c2) {
		t.Fatalf("expected entities with same ID to be equal regardless of attributes")
	}
	// Different ID -> Not equal (even with same attributes)
	if c1.Equals(c3) {
		t.Fatalf("expected entities with different ID not to be equal")
	}
	if c1.Equals(nil) {
		t.Fatalf("expected entity not to equal nil")
	}
}

func TestAggregateRoot_DomainEvents(t *testing.T) {
	acc := NewAccount("acc-001", Money{Amount: 500, Currency: "EUR"})

	eventsList := acc.DomainEvents()
	if len(eventsList) != 1 || eventsList[0].ID() != "evt-acc-created" {
		t.Fatalf("expected 1 uncommitted domain event, got %v", eventsList)
	}

	acc.ClearDomainEvents()
	if len(acc.DomainEvents()) != 0 {
		t.Fatalf("expected 0 events after clear")
	}
}

func TestSpecification_Composition(t *testing.T) {
	isAdult := domain.NewSpec(func(age int) bool {
		return age >= 18
	})
	isSenior := domain.NewSpec(func(age int) bool {
		return age >= 65
	})

	// AND
	workingAge := isAdult.And(isSenior.Not())
	if !workingAge.IsSatisfiedBy(30) {
		t.Errorf("expected 30 to be working age")
	}
	if workingAge.IsSatisfiedBy(15) || workingAge.IsSatisfiedBy(70) {
		t.Errorf("expected 15 and 70 not to be working age")
	}

	// OR
	specialDiscount := isSenior.Or(isAdult.Not())
	if !specialDiscount.IsSatisfiedBy(10) || !specialDiscount.IsSatisfiedBy(70) {
		t.Errorf("expected 10 and 70 to receive special discount")
	}
	if specialDiscount.IsSatisfiedBy(30) {
		t.Errorf("expected 30 not to receive special discount")
	}
}

func TestDomainService_Transfer(t *testing.T) {
	svc := &TransferService{}
	accA := NewAccount("A", Money{Amount: 200, Currency: "EUR"})
	accB := NewAccount("B", Money{Amount: 50, Currency: "EUR"})

	res := svc.Transfer(accA, accB, Money{Amount: 100, Currency: "EUR"})
	if !res.IsSuccess() {
		t.Fatalf("transfer failed: %v", res.Error())
	}
	if accA.Balance.Amount != 100 || accB.Balance.Amount != 150 {
		t.Errorf("balances incorrect after transfer: A=%v, B=%v", accA.Balance.Amount, accB.Balance.Amount)
	}

	resFail := svc.Transfer(accA, accB, Money{Amount: 500, Currency: "EUR"})
	if !resFail.IsFailure() {
		t.Fatalf("expected transfer failure with insufficient funds")
	}
}

func TestFactory_Create(t *testing.T) {
	factory := domain.FactoryFunc[*Account, struct {
		ID      string
		Initial float64
	}](func(_ context.Context, p struct {
		ID      string
		Initial float64
	}) result.Result[*Account] {
		if p.ID == "" {
			return result.FailMsg[*Account]("id is required")
		}
		return result.Ok(NewAccount(p.ID, Money{Amount: p.Initial, Currency: "EUR"}))
	})

	ctx := context.Background()
	accRes := factory.Create(ctx, struct {
		ID      string
		Initial float64
	}{ID: "acc-10", Initial: 100})

	if !accRes.IsSuccess() || accRes.MustValue().ID() != "acc-10" {
		t.Fatalf("expected successful factory creation")
	}
}

func TestPagedResult_Calculation(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	paged := domain.NewPagedResult(items, 45, 1, 10)

	if paged.TotalPages != 5 {
		t.Errorf("expected 5 total pages for 45 items with pageSize 10, got %d", paged.TotalPages)
	}
	if len(paged.Items) != 5 {
		t.Errorf("expected 5 items")
	}
}
