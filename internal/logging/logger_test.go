package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoggerLevelsAndFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(WithOutput(&buf), WithLevel(LevelInfo), WithService("svc"))

	logger.Debug("skip")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for debug at info level")
	}

	logger.Info("hello", "correlation_id", "abc", "foo", "bar", "num", 1)
	entry := decodeLastLog(t, buf.Bytes())

	if entry["message"] != "hello" {
		t.Fatalf("unexpected message: %v", entry["message"])
	}
	if entry["correlation_id"] != "abc" {
		t.Fatalf("unexpected correlation id: %v", entry["correlation_id"])
	}
	if entry["service"] != "svc" {
		t.Fatalf("unexpected service: %v", entry["service"])
	}

	fields := entry["fields"].(map[string]interface{})
	if fields["foo"] != "bar" {
		t.Fatalf("expected foo field")
	}
	if int(fields["num"].(float64)) != 1 {
		t.Fatalf("expected num field")
	}
}

func TestLoggerWithContextAndPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(WithOutput(&buf), WithLevel(LevelWarn))
	ctx := WithCorrelationID(context.Background(), "ctxid")

	logger.InfoWithContext(ctx, "skip")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for info at warn level")
	}

	logger.WarnWithContext(ctx, "warned", "k", "v")
	entry := decodeLastLog(t, buf.Bytes())
	if entry["correlation_id"] != "ctxid" {
		t.Fatalf("unexpected context correlation id")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	logger.Panic("boom")
}

func TestLoggerMarshalError(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(WithOutput(&buf), WithLevel(LevelDebug))

	logger.Info("bad", "field", func() {})
	if buf.Len() != 0 {
		t.Fatalf("expected no output when marshal fails")
	}
}

func TestParseFields(t *testing.T) {
	cid, fields := parseFields([]interface{}{"correlation_id", "cid", "foo", 1, 42, "bad"})
	if cid != "cid" {
		t.Fatalf("unexpected correlation id: %s", cid)
	}
	if fields["foo"] != 1 {
		t.Fatalf("expected foo field")
	}
	if len(fields) != 1 {
		t.Fatalf("unexpected fields length: %d", len(fields))
	}
}

func decodeLastLog(t *testing.T, data []byte) map[string]interface{} {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatalf("no log output")
	}
	line := lines[len(lines)-1]
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("failed to decode log entry: %v", err)
	}
	return entry
}
