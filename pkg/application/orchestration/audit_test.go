package orchestration_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

type accountID struct{ domain.UUID }

// account composes traits instead of inheriting a BusinessEntity.
type account struct {
	domain.BaseAggregateRoot[accountID]
	traits.Activation
	traits.Audited
	name  vocab.Name
	limit vocab.Money
}

type accountClosed struct{ domain.EventMeta }

func (accountClosed) EventType() string { return "test.account_closed" }

func newAccount(t *testing.T, name, limit string) *account {
	base, err := domain.NewBaseAggregateRoot("test.account", accountID{domain.NewUUID()})
	if err != nil {
		t.Fatal(err)
	}
	return &account{BaseAggregateRoot: base, name: vocab.MustName(name), limit: vocab.MustMoney(limit, "EUR")}
}

func (a *account) Close() {
	if a.Deactivate() {
		a.Raise(accountClosed{EventMeta: a.NewEventMeta()})
	}
}

func (a *account) AuditSnapshot() map[string]any {
	return map[string]any{"name": a.name.String(), "limit": a.limit, "active": a.IsActive()}
}

type auditFixture struct {
	store *memory.Store
	log   *memory.AuditLog
	orch  *orchestration.Orchestrator[accountID, *account]
}

func newAuditFixture(t *testing.T) auditFixture {
	store := memory.NewStore("audit")
	log := memory.NewAuditLog(store)
	repo := memory.NewRepository[accountID, *account](store)
	return auditFixture{store, log, orchestration.New[accountID, *account](repo, store, orchestration.WithAuditLog(log))}
}

func TestOrchestrator_AuditTrail(t *testing.T) {
	restore := domain.SetClock(fixedClock(time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)))
	defer restore()
	f := newAuditFixture(t)
	ana, _ := vocab.NewActor(domain.NewUUID(), "Ana")
	luis, _ := vocab.NewActor(domain.NewUUID(), "Luis")

	ctx := application.WithChannel(application.WithActor(context.Background(), ana), "WEB")
	acc := newAccount(t, "Main", "1000")
	if err := f.orch.Create(ctx, acc); err != nil {
		t.Fatal(err)
	}
	if acc.CreatedBy() != ana || acc.CreatedAt().IsZero() {
		t.Fatalf("creation must be stamped with the context actor: %+v", acc.AuditStamp())
	}

	importCtx := application.WithImportProvenance(application.WithActor(context.Background(), luis),
		application.ImportProvenance{SourceKey: "legacy-crm", SourceFile: "accounts.csv"})
	updated, err := f.orch.Update(importCtx, acc.ID(), func(_ context.Context, a *account) error {
		a.limit = vocab.MustMoney("1500", "EUR")
		a.Close()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ModifiedBy() != luis || updated.CreatedBy() != ana {
		t.Fatalf("modification stamp: %+v", updated.AuditStamp())
	}

	trail, _ := f.log.Trail(ctx, "test.account", acc.ID().String())
	if len(trail) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(trail))
	}
	created, changed := trail[0], trail[1]
	if created.Operation != application.AuditCreated || created.Actor != ana || created.Channel != "web" ||
		created.AggregateVersion != 1 || len(created.Changes) != 3 {
		t.Fatalf("creation record: %+v", created)
	}
	fields := []string{}
	for _, c := range changed.Changes {
		fields = append(fields, c.Field)
	}
	if changed.Operation != application.AuditUpdated || changed.Actor != luis || changed.AggregateVersion != 2 ||
		!slices.Equal(fields, []string{"active", "limit"}) || !slices.Equal(changed.Events, []string{"test.account_closed"}) ||
		changed.Import == nil || changed.Import.SourceKey != "legacy-crm" || changed.Import.RunID.IsZero() {
		t.Fatalf("update record: %+v", changed)
	}

	if err := f.orch.Delete(ctx, acc.ID(), nil); err != nil {
		t.Fatal(err)
	}
	trail, _ = f.log.Trail(ctx, "test.account", acc.ID().String())
	if last := trail[len(trail)-1]; last.Operation != application.AuditDeleted || len(last.Changes) != 3 || last.Changes[0].New != nil {
		t.Fatalf("deletion record: %+v", last)
	}
}

func TestOrchestrator_AuditRolledBackWithTheCommand(t *testing.T) {
	f := newAuditFixture(t)
	acc := newAccount(t, "Main", "10")
	if err := f.orch.Create(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	_, err := f.orch.Update(context.Background(), acc.ID(), func(_ context.Context, a *account) error {
		a.Close()
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	trail, _ := f.log.Trail(context.Background(), "test.account", acc.ID().String())
	if len(trail) != 1 {
		t.Fatalf("failed commands must not leave audit records: %d", len(trail))
	}
	if application.ActorFrom(context.Background()) != vocab.SystemActor || trail[0].Actor != vocab.SystemActor {
		t.Fatal("without an actor in the context the system actor is recorded")
	}
}

type fixedClock time.Time

func (f fixedClock) Now() time.Time { return time.Time(f) }
