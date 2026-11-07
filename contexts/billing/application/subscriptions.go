package application

import (
	"context"
	"errors"
	"fmt"
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

// Monthly es la periodicidad de facturación de un cliente que recibe una factura al cerrar el
// periodo, con todos sus albaranes; la otra, per_delivery, un borrador por albarán (D-146). La
// decide Orders en las condiciones del cliente y la lleva el albarán.
const Monthly = "monthly"

// DeliveryIssued es la copia en Billing de orders.delivery-issued.v1 (sólo los campos que necesita).
type DeliveryIssued struct {
	DeliveryID  string `json:"deliveryId"`
	OrderNumber string `json:"orderNumber"`
	Company     string `json:"company"`
	Customer    string `json:"customer"`
	Number      string `json:"number"`
	Date        string `json:"date"`
	// BillingPeriodicity es per_delivery (o vacía, en los albaranes de antes de D-146) o monthly.
	BillingPeriodicity string `json:"billingPeriodicity"`
	Lines              []struct {
		SKU         string `json:"sku"`
		Description string `json:"description"`
		TaxCode     string `json:"taxCode"`
		Quantity    string `json:"quantity"`
		NetPrice    string `json:"netPrice"`
	} `json:"lines"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// maxListedRefs es lo más largo que puede ser la lista de albaranes en una descripción; con más,
// sólo el primero, el último y cuántos son (la lista entera está en los orígenes).
const maxListedRefs = 200

// deliveryDescription es la descripción de la operación de un borrador de albaranes: cita el
// albarán y su pedido; o los albaranes («Albaranes A-1, A-2»); o, si son muchos, el primero, el
// último y cuántos («Albaranes A-1 a A-40 (40)»). Siempre cabe en los 500 caracteres.
func deliveryDescription(number, order string, sources []domain.Source) string {
	if len(sources) == 1 {
		return "Albarán " + number + " (pedido " + order + ")"
	}
	refs := make([]string, 0, len(sources))
	for _, s := range sources {
		refs = append(refs, s.Ref)
	}
	if list := strings.Join(refs, ", "); len(list) <= maxListedRefs {
		return "Albaranes " + list
	}
	return fmt.Sprintf("Albaranes %s a %s (%d)", refs[0], refs[len(refs)-1], len(refs))
}

// deliveryDraft prepara el borrador de uno o varios albaranes: sus líneas, con sus precios netos
// (con al menos 4 decimales del precio, D-144) y códigos de impuesto, y date como fecha de la
// operación.
func (s service) deliveryDraft(ctx context.Context, seller domain.OrganizationID, customer domain.PartyID, description string, date vocab.Date,
	sources []domain.Source, lines []domain.LineInput) (*domain.Invoice, error) {
	settings, err := s.settings(ctx, seller)
	if err != nil {
		return nil, err
	}
	// El precio neto del albarán va con los decimales del pedido (4) o más, si la empresa pide
	// más: con menos, redondearlo perdería dinero (1,2345 × 1000 daría 1230,00 y no 1234,50), y
	// rechazarlo repetiría el evento sin fin.
	calc := settings.Calculation
	calc.PriceDecimals = max(calc.PriceDecimals, domain.DefaultPriceDecimals)
	inv, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: seller, Customer: customer, Kind: domain.Ordinary,
		Description: description, OperationDate: date, Sources: sources, Calculation: calc})
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		l.UnitPrice = l.UnitPrice.Round(int32(calc.PriceDecimals)) // ya los tiene: Orders guarda 4
		if _, err := inv.AddLine(l); err != nil {
			return nil, err
		}
	}
	return inv, nil
}

// Subscribe prepara el borrador de la factura de cada albarán (decisión aprobada 5 de
// docs/PEDIDOS.md: Orders publica, Billing factura). El borrador toma las líneas del albarán con sus
// precios netos y códigos de impuesto, con la fecha de entrega como fecha de la operación; una
// persona lo revisa y lo emite en una serie. Un borrador por albarán: los mensajes entregados otra
// vez no hacen daño. El albarán de un cliente con facturación mensual no tiene borrador: queda
// pendiente hasta el cierre del periodo (ClosePeriod, D-146).
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	messaging.Handle(c, func(ctx context.Context, e DeliveryIssued, _ app.Envelope) error {
		seller, err1 := fw.ParseUUID(e.Company)
		customer, err2 := fw.ParseUUID(e.Customer)
		date, err3 := vocab.ParseDate(e.Date)
		delivery, err4 := fw.ParseUUID(e.DeliveryID)
		if err := errors.Join(err1, err2, err3, err4); err != nil || e.DeliveryID == "" {
			return fw.Violation("billing.invalid_event", "orders.delivery-issued.v1: "+e.DeliveryID)
		}
		var lines []domain.LineInput
		for _, l := range e.Lines {
			q, err1 := vocab.ParseDecimal(l.Quantity)
			price, err2 := vocab.ParseDecimal(l.NetPrice)
			if err := errors.Join(err1, err2); err != nil {
				return fw.Violation("billing.invalid_event", "orders.delivery-issued.v1: "+err.Error())
			}
			lines = append(lines, domain.LineInput{Description: strings.TrimSpace(l.SKU + " " + l.Description), Quantity: q, UnitPrice: price,
				TaxCode: l.TaxCode})
		}
		sid := domain.OrganizationID{UUID: seller}
		done, err := s.Invoices.Exists(ctx, spec.And(domain.InvFieldSeller.Eq(sid), domain.FromSource(DeliverySource, e.DeliveryID)))
		if err != nil || done {
			return err
		}
		if e.BillingPeriodicity == Monthly {
			id := domain.PendingDeliveryID{UUID: delivery}
			if _, err := s.Pending.Get(ctx, id); !errors.Is(err, fw.ErrNotFound) {
				return err // ya pendiente (nil) o un error
			}
			p, err := domain.ReconstitutePendingDelivery(id, domain.PendingDeliveryState{Seller: sid, Customer: domain.PartyID{UUID: customer},
				Number: e.Number, OrderNumber: e.OrderNumber, Date: date, Lines: lines})
			if err != nil {
				return err
			}
			return s.pending.Create(ctx, p)
		}
		sources := []domain.Source{{Type: DeliverySource, ID: e.DeliveryID, Ref: e.Number}}
		inv, err := s.deliveryDraft(ctx, sid, domain.PartyID{UUID: customer}, deliveryDescription(e.Number, e.OrderNumber, sources), date, sources, lines)
		if err != nil {
			return err
		}
		return s.invoices.Create(ctx, inv)
	})
}
