package application_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
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

func TestMediator_UnregisteredRequest(t *testing.T) {
	ctx := context.Background()
	m := application.NewMediator()

	res := application.Send[string](ctx, m, GetPartyQuery{PartyID: "123"})
	if !res.IsFailure() {
		t.Fatalf("expected failure for unregistered request")
	}
}
