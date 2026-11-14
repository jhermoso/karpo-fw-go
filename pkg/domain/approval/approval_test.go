package approval_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
)

var (
	company   = domain.NewUUID()
	requester = domain.NewUUID()
	approver  = domain.NewUUID()
)

func pending(t *testing.T) *approval.Request {
	t.Helper()
	r, err := approval.New(approval.NewRequestID(), company, "purchases.supplier.block",
		approval.Target{Type: "purchases.supplier", ID: "42"}, []byte(`{"reason":"impago"}`), "impago", requester)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func isRule(err error, code string) bool {
	var v *domain.RuleViolationError
	return errors.As(err, &v) && v.Code == code
}

func TestNew_RaisesRequested(t *testing.T) {
	r := pending(t)
	if r.Status() != approval.StatusPending {
		t.Fatalf("status = %s", r.Status())
	}
	ev := r.PendingEvents()
	if len(ev) != 1 || ev[0].EventType() != approval.EventRequested {
		t.Fatalf("events = %v", ev)
	}
}

func TestNew_Validates(t *testing.T) {
	target := approval.Target{Type: "t", ID: "1"}
	cases := map[string]func() error{
		"action": func() error {
			_, e := approval.New(approval.NewRequestID(), company, "block", target, nil, "", requester)
			return e
		},
		"target": func() error {
			_, e := approval.New(approval.NewRequestID(), company, "a.b.c", approval.Target{}, nil, "", requester)
			return e
		},
		"payload": func() error {
			_, e := approval.New(approval.NewRequestID(), company, "a.b.c", target, []byte("{"), "", requester)
			return e
		},
		"company": func() error {
			_, e := approval.New(approval.NewRequestID(), domain.UUID{}, "a.b.c", target, nil, "", requester)
			return e
		},
	}
	for name, fn := range cases {
		if err := fn(); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestApprove_RaisesApprovedWithPayload(t *testing.T) {
	r := pending(t)
	if err := r.Approve(approver, ""); err != nil {
		t.Fatal(err)
	}
	ev := r.PendingEvents()
	a, ok := ev[len(ev)-1].(approval.Approved)
	if !ok || a.ResolvedBy != approver || a.Action != "purchases.supplier.block" || string(a.Payload) != `{"reason":"impago"}` {
		t.Fatalf("event = %#v", ev[len(ev)-1])
	}
	if s := r.State(); s.Status != approval.StatusApproved || s.ResolvedAt.IsZero() {
		t.Fatalf("state = %#v", s)
	}
}

func TestRequesterCannotResolve(t *testing.T) {
	r := pending(t)
	if err := r.Approve(requester, ""); !isRule(err, approval.CodeSelfResolution) {
		t.Fatalf("approve: %v", err)
	}
	if err := r.Reject(requester, "no"); !isRule(err, approval.CodeSelfResolution) {
		t.Fatalf("reject: %v", err)
	}
	if r.Status() != approval.StatusPending {
		t.Fatal("status changed")
	}
}

func TestRejectNeedsComment(t *testing.T) {
	r := pending(t)
	if err := r.Reject(approver, "  "); !isRule(err, approval.CodeCommentRequired) {
		t.Fatalf("err = %v", err)
	}
	if err := r.Reject(approver, "sin justificante"); err != nil || r.Status() != approval.StatusRejected {
		t.Fatalf("err = %v, status = %s", err, r.Status())
	}
}

func TestCancel_OnlyRequester(t *testing.T) {
	r := pending(t)
	if err := r.Cancel(approver, ""); !isRule(err, approval.CodeNotRequester) {
		t.Fatalf("err = %v", err)
	}
	if err := r.Cancel(requester, "ya no hace falta"); err != nil || r.Status() != approval.StatusCancelled {
		t.Fatalf("err = %v, status = %s", err, r.Status())
	}
}

func TestResolvedOnlyOnce(t *testing.T) {
	r := pending(t)
	if err := r.Approve(approver, ""); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"approve": r.Approve(domain.NewUUID(), ""),
		"reject":  r.Reject(domain.NewUUID(), "x"),
		"cancel":  r.Cancel(requester, ""),
	} {
		if !isRule(err, approval.CodeAlreadyResolved) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if r.Status() != approval.StatusApproved {
		t.Fatal("status changed")
	}
}
