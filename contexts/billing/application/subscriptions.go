package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
		// UnitPrice y Discount son el precio y el descuento encadenado de la línea (D-149); un
		// albarán de antes no los lleva y se factura a NetPrice sin descuento.
		UnitPrice string `json:"unitPrice"`
		Discount  string `json:"discount"`
		NetPrice  string `json:"netPrice"`
	} `json:"lines"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// maxListedRefs es lo más largo que puede ser la lista de albaranes en una descripción; con más,
// sólo el primero, el último y cuántos son (la lista entera está en los orígenes).
const maxListedRefs = 200

// deliveryDescription es la descripción de la operación de un borrador de albaranes: cita el
// albarán y su pedido; o los albaranes, por orden de número («Albaranes A-1, A-2»); o, si son
// muchos, el menor, el mayor y cuántos («Albaranes A-1 a A-40 (40)»). Siempre cabe en los 500
// caracteres.
func deliveryDescription(number, order string, sources []domain.Source) string {
	if len(sources) == 1 {
		return "Albarán " + number + " (pedido " + order + ")"
	}
	refs := make([]string, 0, len(sources))
	for _, s := range sources {
		refs = append(refs, s.Ref)
	}
	slices.Sort(refs)
	if list := strings.Join(refs, ", "); len(list) <= maxListedRefs {
		return "Albaranes " + list
	}
	return fmt.Sprintf("Albaranes %s a %s (%d)", refs[0], refs[len(refs)-1], len(refs))
}

// deliveryDraft prepara el borrador de uno o varios albaranes: sus líneas, con su precio (con al
// menos 4 decimales, D-144), su descuento y su código de impuesto, y date como fecha de la
// operación.
func (s service) deliveryDraft(ctx context.Context, seller domain.OrganizationID, customer domain.PartyID, description string, date vocab.Date,
	sources []domain.Source, lines []domain.LineInput) (*domain.Invoice, error) {
	settings, err := s.settings(ctx, seller)
	if err != nil {
		return nil, err
	}
	// El precio del albarán va con los decimales del pedido (4) o más, si la empresa pide más: con
	// menos, redondearlo perdería dinero (1,2345 × 1000 daría 1230,00 y no 1234,50), y rechazarlo
	// repetiría el evento sin fin.
	calc, err := s.calculation(ctx, settings, seller, customer)
	if err != nil {
		return nil, err
	}
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

// deliveryLine lee la cantidad, el precio y el descuento de una línea de albarán: el precio y el
// descuento encadenado si los trae (D-149) o, en un albarán de antes, el precio neto sin descuento.
func deliveryLine(quantity, unitPrice, discount, netPrice string) (domain.LineInput, error) {
	q, err := vocab.ParseDecimal(quantity)
	if err != nil {
		return domain.LineInput{}, err
	}
	if unitPrice == "" {
		price, err := vocab.ParseDecimal(netPrice)
		return domain.LineInput{Quantity: q, UnitPrice: price, Discount: vocab.DecimalFromInt(0)}, err
	}
	price, err := vocab.ParseDecimal(unitPrice)
	if err != nil {
		return domain.LineInput{}, err
	}
	d := vocab.DecimalFromInt(0)
	if discount != "" {
		if d, err = vocab.ParseDecimal(discount); err != nil {
			return domain.LineInput{}, err
		}
	}
	return domain.LineInput{Quantity: q, UnitPrice: price, Discount: d}, nil
}

// Subscribe prepara el borrador de la factura de cada albarán (decisión aprobada 5 de
// docs/PEDIDOS.md: Orders publica, Billing factura). El borrador toma las líneas del albarán con su
// precio, su descuento encadenado (D-149: la factura da lo mismo que el albarán al céntimo) y su
// código de impuesto, con la fecha de entrega como fecha de la operación; una
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
			line, err := deliveryLine(l.Quantity, l.UnitPrice, l.Discount, l.NetPrice)
			if err != nil {
				return fw.Violation("billing.invalid_event", "orders.delivery-issued.v1: "+err.Error())
			}
			line.Description, line.TaxCode = strings.TrimSpace(l.SKU+" "+l.Description), l.TaxCode
			lines = append(lines, line)
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
			// Se prepara su borrador sin guardarlo, para validar sus líneas: un albarán que no se
			// podría facturar se rechaza ahora, como el de un cliente por albarán, y no queda pendiente
			// bloqueando el cierre de su cliente.
			sources := []domain.Source{{Type: DeliverySource, ID: e.DeliveryID, Ref: e.Number}}
			if _, err := s.deliveryDraft(ctx, sid, domain.PartyID{UUID: customer}, deliveryDescription(e.Number, e.OrderNumber, sources), date,
				sources, lines); err != nil {
				return err
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
