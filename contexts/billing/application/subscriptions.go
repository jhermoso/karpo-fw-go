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

// DeliverySource es el tipo de origen de las facturas preparadas a partir de albaranes de Orders.
const DeliverySource = "orders.delivery"

// DeliveryIssued es la copia en Billing de orders.delivery-issued.v1 (sólo los campos que necesita).
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

// IntegrationEventType implementa application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// Subscribe prepara el borrador de la factura de cada albarán (decisión aprobada 5 de
// docs/PEDIDOS.md: Orders publica, Billing factura). El borrador toma las líneas del albarán con sus
// precios netos y códigos de impuesto, con la fecha de entrega como fecha de la operación; una
// persona lo revisa y lo emite en una serie. Un borrador por albarán: los mensajes entregados otra vez no hacen daño.
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
