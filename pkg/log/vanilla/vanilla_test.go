package vanilla_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
)

func TestVanillaLogger_JSON(t *testing.T) {
	var buf bytes.Buffer
	logger := vanilla.NewJSON(&buf, log.LevelDebug)

	logger.Info("user logged in", "user_id", "usr_123", "role", "admin")

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse json log output: %v, raw: %s", err, buf.String())
	}

	if output["msg"] != "user logged in" {
		t.Errorf("expected msg 'user logged in', got: %v", output["msg"])
	}
	if output["user_id"] != "usr_123" {
		t.Errorf("expected user_id 'usr_123', got: %v", output["user_id"])
	}
	if output["level"] != "INFO" {
		t.Errorf("expected level 'INFO', got: %v", output["level"])
	}
}

func TestVanillaLogger_With(t *testing.T) {
	var buf bytes.Buffer
	base := vanilla.NewJSON(&buf, log.LevelInfo)
	scoped := base.With("service", "parties", "tenant", "ten_001")

	scoped.Warn("rate limit nearing threshold", "current", 85)

	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse json log output: %v", err)
	}

	if output["service"] != "parties" || output["tenant"] != "ten_001" {
		t.Errorf("expected contextual attributes, got: %v", output)
	}
	if output["level"] != "WARN" {
		t.Errorf("expected level WARN, got: %v", output["level"])
	}
}
