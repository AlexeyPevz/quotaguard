package logging

import (
	"context"
	"testing"
)

func TestCorrelationIDHelpers(t *testing.T) {
	ctx := context.Background()
	if GetCorrelationID(ctx) != "" {
		t.Fatalf("expected empty correlation id")
	}

	ctx = WithCorrelationID(ctx, "cid")
	if GetCorrelationID(ctx) != "cid" {
		t.Fatalf("expected correlation id to be set")
	}

	if MustGetCorrelationID(ctx) != "cid" {
		t.Fatalf("expected existing correlation id to be returned")
	}

	newID := MustGetCorrelationID(context.Background())
	if newID == "" {
		t.Fatalf("expected generated correlation id")
	}
}

func TestGenerateCorrelationID(t *testing.T) {
	id := GenerateCorrelationID()
	if id == "" {
		t.Fatalf("expected non-empty id")
	}
}
