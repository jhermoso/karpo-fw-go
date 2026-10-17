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

// alertTimeout es cuánto puede tardar avisar a alguien: la ronda de tareas lo espera.
const alertTimeout = 5 * time.Second

// Alerts avisa de lo que necesita a una persona: siempre en el log, y en un webhook cuando el
// entorno da uno. El webhook recibe un objeto JSON con un "text" que una persona puede leer (lo que
// admiten los webhooks entrantes de las herramientas de chat habituales) y los datos aparte, para
// un programa. No se envía nada del mensaje en sí: sólo de quién era, su tipo y por qué se rechazó.
type Alerts struct {
	Webhook string
	Logger  log.Logger
	Client  *http.Client // nil: uno con timeout
}

// alert es lo que recibe el webhook.
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

// DeliveryGivenUp avisa de que se abandonó un mensaje para un oyente. Nunca falla: una alerta que
// no se puede enviar se anota en el log, y la lista de entregas sigue teniendo lo abandonado.
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
