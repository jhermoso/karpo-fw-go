package inprocess_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/transporttest"
)

// The in-process broker honours the transport contract. It runs the consumers inside Send, so
// redelivery and parking are the relay's; it neither shares messages among replicas nor keeps
// them for detached consumers (both are meaningless inside one process).
func TestBroker_Conformance(t *testing.T) {
	transporttest.Run(t, transporttest.Harness{
		New: func(*testing.T) transporttest.Transport {
			b := inprocess.NewBroker()
			return transporttest.Transport{Sender: b, Subscriber: b}
		},
	})
}
