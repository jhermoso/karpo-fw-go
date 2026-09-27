package traits_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type fixed time.Time

func (f fixed) Now() time.Time { return time.Time(f) }

// contract is an aggregate composed from traits instead of a BusinessEntity base class.
type contract struct {
	traits.Activation
	traits.Validity
	traits.Audited
	traits.TestFlag
	amount vocab.Money
	tags   vocab.TagSet
}

func (c *contract) AuditSnapshot() map[string]any {
	return map[string]any{"active": c.IsActive(), "amount": c.amount, "tags": c.tags.String()}
}

var (
	_ traits.Activatable = (*contract)(nil)
	_ traits.Temporal    = (*contract)(nil)
	_ traits.Auditable   = (*contract)(nil)
	_ traits.TestMarked  = (*contract)(nil)
	_ traits.Snapshotter = (*contract)(nil)
)

func TestActivation(t *testing.T) {
	var c contract
	if !c.IsActive() {
		t.Fatal("the zero value must be active (C# default)")
	}
	if c.Activate() {
		t.Fatal("activating an active aggregate is not a change")
	}
	if !c.Deactivate() || c.IsActive() || c.Deactivate() {
		t.Fatal("deactivation must report only real transitions")
	}
	if traits.RestoredActivation(false).IsActive() {
		t.Fatal("restored state")
	}
}

func TestValidity(t *testing.T) {
	jan := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	p, _ := vocab.OpenPeriodFrom(jan)
	c := contract{Validity: traits.NewValidity(p)}

	restore := domain.SetClock(fixed(jan.AddDate(0, 0, 10)))
	defer restore()
	end := jan.AddDate(0, 0, 13)
	if changed, err := c.ExpireAt(end); err != nil || !changed {
		t.Fatalf("expire: %v %v", changed, err)
	}
	if changed, _ := c.ExpireAt(end); changed {
		t.Fatal("same end is not a change")
	}
	if !c.ExpiresWithin(7*24*time.Hour) || c.ExpiresWithin(24*time.Hour) || c.HasExpired() || !c.IsCurrentlyValid() {
		t.Fatal("warning period semantics")
	}
	if _, err := c.ExpireAt(jan.AddDate(0, 0, -1)); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expiring before the start must fail, got %v", err)
	}
	if !c.Reopen() || !c.ValidPeriod().IsOpenEnded() {
		t.Fatal("reopen")
	}
}

func TestAuditStamp(t *testing.T) {
	var c contract
	ana, _ := vocab.NewActor(domain.NewUUID(), "Ana")
	luis, _ := vocab.NewActor(domain.NewUUID(), "Luis")
	t0 := time.Date(2025, 3, 1, 10, 0, 0, 0, time.FixedZone("CET", 3600))

	traits.Stamp(&c, ana, t0)
	if c.CreatedBy() != ana || !c.CreatedAt().Equal(t0) || c.CreatedAt().Location() != time.UTC || !c.ModifiedAt().IsZero() {
		t.Fatalf("first stamp is the creation: %+v", c.AuditStamp())
	}
	traits.Stamp(&c, luis, t0.Add(time.Hour))
	if c.CreatedBy() != ana || c.ModifiedBy() != luis || !c.ModifiedAt().Equal(t0.Add(time.Hour)) {
		t.Fatalf("later stamps are modifications: %+v", c.AuditStamp())
	}
	restored := traits.RestoredAudit(c.AuditStamp())
	if restored.AuditStamp() != c.AuditStamp() {
		t.Fatal("restore")
	}
}

func TestDiff(t *testing.T) {
	c := contract{amount: vocab.MustMoney("10.0", "EUR")}
	before := c.AuditSnapshot()
	c.amount = vocab.MustMoney("10.00", "EUR") // numerically equal: not a change
	c.Deactivate()
	c.tags, _ = vocab.NewTagSet("vip")
	changes := traits.Diff(before, c.AuditSnapshot())
	if len(changes) != 2 || changes[0].Field != "active" || changes[1].Field != "tags" ||
		changes[0].Old != true || changes[0].New != false {
		t.Fatalf("unexpected diff: %+v", changes)
	}
	created := traits.Diff(nil, map[string]any{"a": 1})
	deleted := traits.Diff(map[string]any{"a": 1}, nil)
	if len(created) != 1 || created[0].New != 1 || len(deleted) != 1 || deleted[0].Old != 1 {
		t.Fatalf("creation/deletion diffs: %+v %+v", created, deleted)
	}
}

func TestTestFlag(t *testing.T) {
	var c contract
	c.MarkAsTest()
	if !c.IsTest() || traits.RestoredTestFlag(false).IsTest() {
		t.Fatal("test flag")
	}
}
