package application

import (
	"context"
	"errors"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DeliverySource is the source type of the invoices drafted from delivery notes of Orders.
const DeliverySource = "orders.delivery"

// DeliveryIssued is the Billing copy of orders.delivery-issued.v1 (only the fields it needs).
type DeliveryIssued struct {
	DeliveryID  string `json:"deliveryId"`
	OrderNumber string `json:"orderNumber"`
	Company     string `json:"company"`
	Customer    string `json:"customer"`
	Number      string `json:"number"`
	Date        string `json:"date"`
	Lines       []struct {
		SKU         string `json:"sku"`
		Description string `json:"description"`
		TaxCode     string `json:"taxCode"`
		Quantity    string `json:"quantity"`
		NetPrice    string `json:"netPrice"`
	} `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// Subscribe drafts the invoice of each delivery note (approved decision 5 of docs/PEDIDOS.md:
// Orders publishes, Billing invoices). The draft takes the lines of the delivery note at their net
// prices and tax codes, with the delivery date as the date of the operation; a person reviews it
// and issues it in a series. One draft per delivery note: redeliveries are harmless.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	messaging.Handle(c, func(ctx context.Context, e DeliveryIssued, _ app.Envelope) error {
		seller, err1 := fw.ParseUUID(e.Company)
		customer, err2 := fw.ParseUUID(e.Customer)
		date, err3 := vocab.ParseDate(e.Date)
		if err := errors.Join(err1, err2, err3); err != nil || e.DeliveryID == "" {
			return fw.Violation("billing.invalid_event", "orders.delivery-issued.v1: "+e.DeliveryID)
		}
		sid := domain.OrganizationID{UUID: seller}
		done, err := s.Invoices.Exists(ctx, spec.And(domain.InvFieldSeller.Eq(sid), domain.InvFieldSrcType.Eq(DeliverySource),
			domain.InvFieldSrcID.Eq(e.DeliveryID)))
		if err != nil || done {
			return err
		}
		inv, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: sid, Customer: domain.PartyID{UUID: customer}, Kind: domain.Ordinary,
			Description: "Albarán " + e.Number + " (pedido " + e.OrderNumber + ")", OperationDate: date,
			Source: domain.Source{Type: DeliverySource, ID: e.DeliveryID, Ref: e.Number}})
		if err != nil {
			return err
		}
		for _, l := range e.Lines {
			q, err1 := vocab.ParseDecimal(l.Quantity)
			price, err2 := vocab.ParseDecimal(l.NetPrice)
			if err := errors.Join(err1, err2); err != nil {
				return fw.Violation("billing.invalid_event", "orders.delivery-issued.v1: "+err.Error())
			}
			if _, err := inv.AddLine(domain.LineInput{Description: strings.TrimSpace(l.SKU + " " + l.Description), Quantity: q, UnitPrice: price,
				TaxCode: l.TaxCode}); err != nil {
				return err
			}
		}
		return s.invoices.Create(ctx, inv)
	})
}
