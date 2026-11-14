package approval_test

import (
	"context"
	"errors"
	"testing"

	app "github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

const action approval.Action = "purchases.supplier.block"

func as(t *testing.T, party domain.UUID, perms ...authz.Permission) context.Context {
	t.Helper()
	c, err := authz.NewContext(authz.Context{Subject: domain.NewUUID(), SubjectName: "u", Kind: authz.Human,
		ActorPartyID: party, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	return authz.WithContext(context.Background(), c)
}

func setup(t *testing.T) (app.Service, *approval.Request, domain.UUID) {
	t.Helper()
	svc := app.Service{Repo: memory.NewRepository[approval.RequestID, *approval.Request](memory.NewStore("t"))}
	requester := domain.NewUUID()
	r, err := svc.Request(as(t, requester), app.RequestInput{Company: domain.NewUUID(), Action: action,
		Target: approval.Target{Type: "purchases.supplier", ID: "42"}, Reason: "impago"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, r, requester
}

func TestApprovePermission(t *testing.T) {
	if got := app.ApprovePermission("purchases.supplier.block"); got != "Purchases.Supplier.ApproveBlock" {
		t.Fatalf("got %s", got)
	}
	if got := app.ApprovePermission("sales.credit_limit.raise"); got != "Sales.CreditLimit.ApproveRaise" || !got.WellFormed() {
		t.Fatalf("got %s", got)
	}
}

func TestPolicies(t *testing.T) {
	ctx := context.Background()
	if req, _ := (app.Never{}).Requires(ctx, domain.NewUUID(), action); req {
		t.Fatal("never requires")
	}
	f := app.Fixed{action: true}
	if req, _ := f.Requires(ctx, domain.NewUUID(), action); !req {
		t.Fatal("fixed should require")
	}
	if req, _ := f.Requires(ctx, domain.NewUUID(), "purchases.supplier.unblock"); req {
		t.Fatal("fixed should not require others")
	}
}

func TestRequest_RecordsRequester(t *testing.T) {
	_, r, requester := setup(t)
	if r.RequestedBy() != requester || r.Status() != approval.StatusPending {
		t.Fatalf("state = %#v", r.State())
	}
}

func TestApprove_NeedsPermission(t *testing.T) {
	svc, r, _ := setup(t)
	if _, err := svc.Approve(as(t, domain.NewUUID(), "Purchases.Supplier.Block"), r.ID(), ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	got, err := svc.Approve(as(t, domain.NewUUID(), app.ApprovePermission(action)), r.ID(), "ok")
	if err != nil || got.Status() != approval.StatusApproved {
		t.Fatalf("err = %v", err)
	}
	stored, _ := svc.Repo.Get(context.Background(), r.ID())
	if stored.Status() != approval.StatusApproved || stored.Version() != 2 {
		t.Fatalf("stored = %s v%d", stored.Status(), stored.Version())
	}
}

func TestApprove_NotByRequesterEvenWithPermission(t *testing.T) {
	svc, r, requester := setup(t)
	_, err := svc.Approve(as(t, requester, app.ApprovePermission(action)), r.ID(), "")
	if !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("err = %v", err)
	}
}

func TestReject_NeedsComment(t *testing.T) {
	svc, r, _ := setup(t)
	ctx := as(t, domain.NewUUID(), app.ApprovePermission(action))
	if _, err := svc.Reject(ctx, r.ID(), ""); !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.Reject(ctx, r.ID(), "sin justificante"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTwice(t *testing.T) {
	svc, r, _ := setup(t)
	ctx := as(t, domain.NewUUID(), app.ApprovePermission(action))
	if _, err := svc.Approve(ctx, r.ID(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reject(ctx, r.ID(), "tarde"); !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancel_ByRequesterWithoutPermission(t *testing.T) {
	svc, r, requester := setup(t)
	got, err := svc.Cancel(as(t, requester), r.ID(), "")
	if err != nil || got.Status() != approval.StatusCancelled {
		t.Fatalf("err = %v", err)
	}
}

func TestNoAuthorizationContext(t *testing.T) {
	svc, r, _ := setup(t)
	if _, err := svc.Approve(context.Background(), r.ID(), ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}
