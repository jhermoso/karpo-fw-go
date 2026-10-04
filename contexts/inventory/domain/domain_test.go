package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

var (
	company   = domain.OrganizationID{UUID: fw.NewUUID()}
	warehouse = domain.NewWarehouseID()
	product   = domain.ProductID{UUID: fw.NewUUID()}
)

func level(t *testing.T) *domain.Level {
	t.Helper()
	l, err := domain.OpenLevel(domain.NewLevelID(), company, warehouse, product)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestWarehouse(t *testing.T) {
	if _, err := domain.ReconstituteWarehouse(domain.NewWarehouseID(), domain.WarehouseState{Company: company, Code: "AL 1", Name: "Central"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("code with a space: %v", err)
	}
	w, err := domain.ReconstituteWarehouse(domain.NewWarehouseID(), domain.WarehouseState{Company: company, Code: " al1 ", Name: "Central", Active: true})
	if err != nil || w.State().Code != "AL1" {
		t.Fatalf("warehouse: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !isViolation(err, "inventory.warehouse_closed") {
		t.Fatalf("closed twice: %v", err)
	}
}

func TestLevel_WeightedAverageCost(t *testing.T) {
	l := level(t)
	if _, err := l.Receive(dec("0"), dec("1")); !isViolation(err, "inventory.quantity") {
		t.Fatalf("zero: %v", err)
	}
	if _, err := l.Receive(dec("1"), dec("-1")); !isViolation(err, "inventory.quantity") {
		t.Fatalf("negative cost: %v", err)
	}
	if seq, err := l.Receive(dec("100"), dec("2.00")); err != nil || seq != 1 {
		t.Fatalf("receive: %d %v", seq, err)
	}
	if _, err := l.Receive(dec("300"), dec("3.00")); err != nil {
		t.Fatal(err)
	}
	// (100×2 + 300×3) / 400 = 2.75
	if s := l.State(); !s.OnHand.Equal(dec("400")) || !s.AverageCost.Equal(dec("2.75")) || !l.Value().Equal(dec("1100")) {
		t.Fatalf("average: %+v", s)
	}
	cost, seq, err := l.Issue(dec("150"))
	if err != nil || !cost.Equal(dec("2.75")) || seq != 3 || !l.State().OnHand.Equal(dec("250")) {
		t.Fatalf("issue: %s %d %v", cost, seq, err)
	}
	// An issue does not change the average; the next receipt does: (250×2.75 + 250×3.25) / 500 = 3.
	if _, err := l.Receive(dec("250"), dec("3.25")); err != nil || !l.State().AverageCost.Equal(dec("3")) {
		t.Fatalf("second average: %s %v", l.State().AverageCost, err)
	}
	if _, _, err := l.Issue(dec("500.0001")); !isViolation(err, "inventory.insufficient_stock") {
		t.Fatalf("more than there is: %v", err)
	}
}

func TestLevel_ReservationsAndCounts(t *testing.T) {
	l := level(t)
	if _, err := l.Receive(dec("10"), dec("5")); err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve(dec("11")); !isViolation(err, "inventory.insufficient_stock") {
		t.Fatalf("reserve more than available: %v", err)
	}
	if err := l.Reserve(dec("6")); err != nil || !l.Available().Equal(dec("4")) {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := l.Issue(dec("5")); !isViolation(err, "inventory.insufficient_stock") {
		t.Fatalf("what is reserved is not taken: %v", err)
	}
	if _, _, err := l.IssueReserved(dec("7")); !isViolation(err, "inventory.reservation_exceeded") {
		t.Fatalf("issue more than reserved: %v", err)
	}
	if _, _, err := l.IssueReserved(dec("2")); err != nil || !l.State().OnHand.Equal(dec("8")) || !l.State().Reserved.Equal(dec("4")) {
		t.Fatalf("issue reserved: %+v %v", l.State(), err)
	}
	if _, _, err := l.CountTo(dec("3")); !isViolation(err, "inventory.count_under_reserved") {
		t.Fatalf("count under reserved: %v", err)
	}
	if err := l.Release(dec("5")); !isViolation(err, "inventory.reservation_exceeded") {
		t.Fatalf("release too much: %v", err)
	}
	if err := l.Release(dec("4")); err != nil {
		t.Fatal(err)
	}
	if diff, seq, err := l.CountTo(dec("8")); err != nil || !diff.IsZero() || seq != 0 {
		t.Fatalf("count without difference: %s %d %v", diff, seq, err)
	}
	diff, seq, err := l.CountTo(dec("6.5"))
	if err != nil || !diff.Equal(dec("-1.5")) || seq != 3 || !l.State().AverageCost.Equal(dec("5")) {
		t.Fatalf("count: %s %d %v", diff, seq, err)
	}
	if l.BelowReorder() {
		t.Fatal("no reorder point yet")
	}
	if err := l.SetReorderPoint(dec("6.5")); err != nil || !l.BelowReorder() {
		t.Fatalf("reorder: %v", err)
	}
}

func TestMovementAndReservation(t *testing.T) {
	l := level(t)
	seq, _ := l.Receive(dec("3"), dec("1.3333"))
	m, err := domain.Post(domain.NewMovementID(), l, seq, domain.Receipt, date("2026-10-04"), dec("3"), dec("1.3333"), " L-01 ", "", domain.Source{})
	if err != nil {
		t.Fatal(err)
	}
	if s := m.State(); !s.Value.Equal(dec("4")) || !s.Balance.Equal(dec("3")) || s.Lot != "L-01" || s.Seq != 1 {
		t.Fatalf("movement: %+v", s)
	}
	if _, err := domain.Post(domain.NewMovementID(), l, 2, domain.Issue, vocab.Date{}, dec("-1"), dec("1"), "", "", domain.Source{}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("no date: %v", err)
	}

	r, err := domain.ReconstituteReservation(domain.NewReservationID(), domain.ReservationState{Company: company, Warehouse: warehouse, Product: product,
		Source: domain.Source{Type: "order", ID: "1"}, Quantity: dec("5"), Open: dec("5")})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Consume(dec("6")); !isViolation(err, "inventory.reservation_exceeded") {
		t.Fatalf("consume too much: %v", err)
	}
	if err := r.Consume(dec("2")); err != nil {
		t.Fatal(err)
	}
	if q, err := r.Release(); err != nil || !q.Equal(dec("3")) {
		t.Fatalf("release: %s %v", q, err)
	}
	if _, err := r.Release(); !isViolation(err, "inventory.reservation_closed") {
		t.Fatalf("release twice: %v", err)
	}
}
