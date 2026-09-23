package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// 1. CreatePartyCommand
type CreatePartyCommand struct {
	application.BaseCommand[string]
	TaxID string
	Name  string
}

// 2. GetPartyQuery
type GetPartyQuery struct {
	application.BaseQuery[string]
	PartyID string
}

// 3. Mock Aggregate Root for Orchestrator test
type MockAccount struct {
	domain.BaseAggregateRoot[string]
	Balance float64
}

func NewMockAccount(id string, balance float64) *MockAccount {
	acc := &MockAccount{
		BaseAggregateRoot: domain.NewBaseAggregateRoot(id),
		Balance:           balance,
	}
	acc.AddDomainEvent(events.BaseEvent{
		EventID:        "evt-created",
		EventType:      "mock.account.created",
		EventTimestamp: time.Now(),
	})
	return acc
}

func (a *MockAccount) Deposit(amount float64) {
	a.Balance += amount
	a.AddDomainEvent(events.BaseEvent{
		EventID:        "evt-deposited",
		EventType:      "mock.account.deposited",
		EventTimestamp: time.Now(),
	})
}

func TestMediator_CommandAndQueryDispatch(t *testing.T) {
	ctx := context.Background()
	m := application.NewMediator()

	// Register Command Handler
	application.RegisterCommandHandler(m, application.CommandHandlerFunc[CreatePartyCommand, string](
		func(_ context.Context, cmd CreatePartyCommand) result.Result[string] {
			if cmd.TaxID == "" {
				return result.FailMsg[string]("tax id is required")
			}
			return result.Ok("party-created-" + cmd.TaxID)
		},
	))

	// Register Query Handler
	application.RegisterQueryHandler(m, application.QueryHandlerFunc[GetPartyQuery, string](
		func(_ context.Context, q GetPartyQuery) result.Result[string] {
			if q.PartyID == "p-1" {
				return result.Ok("Acme Corp")
			}
			return result.FailMsg[string]("party not found")
		},
	))

	// Dispatch Command
	cmdRes := application.Send[string](ctx, m, CreatePartyCommand{TaxID: "B12345678", Name: "Acme"})
	if !cmdRes.IsSuccess() || cmdRes.MustValue() != "party-created-B12345678" {
		t.Fatalf("expected successful command, got: %v", cmdRes)
	}

	// Dispatch Command with failure
	cmdFail := application.Send[string](ctx, m, CreatePartyCommand{TaxID: ""})
	if !cmdFail.IsFailure() || cmdFail.Error().Error() != "tax id is required" {
		t.Fatalf("expected command failure, got: %v", cmdFail)
	}

	// Dispatch Query
	queryRes := application.Send[string](ctx, m, GetPartyQuery{PartyID: "p-1"})
	if !queryRes.IsSuccess() || queryRes.MustValue() != "Acme Corp" {
		t.Fatalf("expected query success, got: %v", queryRes)
	}

	// Dispatch Query not found
	queryNotFound := application.Send[string](ctx, m, GetPartyQuery{PartyID: "p-unknown"})
	if !queryNotFound.IsFailure() {
		t.Fatalf("expected query failure for unknown id")
	}
}

func TestMediator_PipelineBehavior(t *testing.T) {
	ctx := context.Background()
	m := application.NewMediator()

	var trace []string

	// Register Middleware / Pipeline Behavior 1
	m.Use(func(c context.Context, req any, next application.NextFunc) (any, error) {
		trace = append(trace, "behavior1:before")
		res, err := next(c)
		trace = append(trace, "behavior1:after")
		return res, err
	})

	// Register Middleware / Pipeline Behavior 2
	m.Use(func(c context.Context, req any, next application.NextFunc) (any, error) {
		trace = append(trace, "behavior2:before")
		res, err := next(c)
		trace = append(trace, "behavior2:after")
		return res, err
	})

	// Register Handler
	application.RegisterCommandHandler(m, application.CommandHandlerFunc[CreatePartyCommand, string](
		func(_ context.Context, _ CreatePartyCommand) result.Result[string] {
			trace = append(trace, "handler:executed")
			return result.Ok("done")
		},
	))

	res := application.Send[string](ctx, m, CreatePartyCommand{TaxID: "X"})
	if !res.IsSuccess() || res.MustValue() != "done" {
		t.Fatalf("expected success")
	}

	expectedTrace := []string{
		"behavior1:before",
		"behavior2:before",
		"handler:executed",
		"behavior2:after",
		"behavior1:after",
	}

	if len(trace) != len(expectedTrace) {
		t.Fatalf("trace mismatch: expected %v, got %v", expectedTrace, trace)
	}
	for i, step := range expectedTrace {
		if trace[i] != step {
			t.Errorf("step %d mismatch: expected %s, got %s", i, step, trace[i])
		}
	}
}

func TestOrchestrator_Lifecycle(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository[string, *MockAccount](func(a *MockAccount) string {
		return a.ID()
	})
	uow := &memory.MemoryUnitOfWork{}
	bus := inprocess.New()

	var publishedEvents []string
	bus.Subscribe("*", events.HandlerFunc(func(_ context.Context, evt events.Event) error {
		publishedEvents = append(publishedEvents, evt.Type())
		return nil
	}))

	orchestrator := application.NewOrchestrator[string, *MockAccount](repo, uow, bus)

	// 1. Create Aggregate via Orchestrator
	newAcc := NewMockAccount("acc-100", 250.0)
	createRes := orchestrator.Create(ctx, newAcc)
	if !createRes.IsSuccess() {
		t.Fatalf("create failed: %v", createRes.Error())
	}

	if len(publishedEvents) != 1 || publishedEvents[0] != "mock.account.created" {
		t.Fatalf("expected account.created event dispatched, got: %v", publishedEvents)
	}
	if len(newAcc.DomainEvents()) != 0 {
		t.Fatalf("expected domain events cleared after orchestrator create")
	}

	// 2. Mutate Aggregate via Orchestrator
	mutateRes := orchestrator.Mutate(ctx, "acc-100", func(agg *MockAccount) result.Result[any] {
		agg.Deposit(100.0)
		return result.Ok[any](agg.Balance)
	})

	if !mutateRes.IsSuccess() || mutateRes.MustValue() != 350.0 {
		t.Fatalf("mutation failed: %v", mutateRes)
	}

	if len(publishedEvents) != 2 || publishedEvents[1] != "mock.account.deposited" {
		t.Fatalf("expected account.deposited event dispatched, got: %v", publishedEvents)
	}

	// Verify persistence in repository
	found := repo.FindByID(ctx, "acc-100")
	if !found.IsSuccess() || found.MustValue().Balance != 350.0 {
		t.Fatalf("expected balance 350 persisted in repository")
	}
}
