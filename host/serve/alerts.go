package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jhermoso/karpo-fw-go/host/mailbox"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// alertTimeout is how long telling somebody may take: the round of chores waits for it.
const alertTimeout = 5 * time.Second

// Alerts tells what needs a person: always in the log, and at a webhook when the environment
// gives one. The webhook gets a JSON object with a "text" a person can read (what the incoming
// webhooks of the usual chat tools take) and the facts apart, for a program. Nothing of the
// message itself is sent: only whose it was, its type and why it was refused.
type Alerts struct {
	Webhook string
	Logger  log.Logger
	Client  *http.Client // nil: one with a timeout
}

// alert is what the webhook gets.
type alert struct {
	Text     string `json:"text"`
	Kind     string `json:"kind"`
	Consumer string `json:"consumer"`
	Delivery string `json:"delivery"`
	Event    string `json:"eventType"`
	Subject  string `json:"subject,omitempty"`
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason,omitempty"`
}

// DeliveryGivenUp tells that a message was given up for a listener. It never fails: an alert that
// cannot be sent is logged, and the list of deliveries still has what was given up.
func (a Alerts) DeliveryGivenUp(ctx context.Context, d mailbox.DTO) {
	if a.Logger != nil {
		a.Logger.Error("a message was given up for a listener: it waits for somebody to retry or discard it", "consumer", d.Consumer,
			"delivery", d.ID, "eventType", d.EventType, "subject", d.Subject, "attempts", d.Attempts, "reason", d.Reason)
	}
	if a.Webhook == "" {
		return
	}
	body, _ := json.Marshal(alert{Kind: "delivery-given-up", Consumer: d.Consumer, Delivery: d.ID, Event: d.EventType, Subject: d.Subject,
		Attempts: d.Attempts, Reason: d.Reason,
		Text: fmt.Sprintf("Karpo: %s no pudo recibir un mensaje %s tras %d intentos y ya no se reintenta solo. Motivo: %s. Hay que reintentarlo o descartarlo (entrega %s).",
			d.Consumer, d.EventType, d.Attempts, d.Reason, d.ID)})
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Webhook, bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		client := a.Client
		if client == nil {
			client = &http.Client{Timeout: alertTimeout}
		}
		var res *http.Response
		if res, err = client.Do(req); err == nil {
			_ = res.Body.Close()
			if res.StatusCode >= 300 {
				err = fmt.Errorf("the webhook answered %d", res.StatusCode)
			}
		}
	}
	if err != nil && a.Logger != nil {
		a.Logger.Error("the alert could not be sent to the webhook", "error", err.Error(), "delivery", d.ID)
	}
}
