package logging

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteAuditStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.db")
	store, err := NewSQLiteAuditStoreWithRetention(path, 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	store.logger = NewLogger(WithOutput(&bytes.Buffer{}), WithLevel(LevelDebug))
	defer store.Close()

	event := NewAuditEvent(APIAccess, "GET /", StatusSuccess)
	event.ID = "event-1"
	event.IPAddress = "127.0.0.1"
	event.Resource = "/"
	event.Timestamp = time.Now().Add(-2 * time.Hour)
	event.Details = map[string]interface{}{"foo": "bar"}

	if err := store.SaveEvent(event); err != nil {
		t.Fatalf("failed to save event: %v", err)
	}

	ctx := context.Background()
	count, err := store.CountEvents(ctx, AuditQueryFilters{EventType: string(APIAccess)})
	if err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	got, err := store.GetEventByID(ctx, "event-1")
	if err != nil {
		t.Fatalf("failed to get event: %v", err)
	}
	if got == nil || got.Resource != "/" {
		t.Fatalf("expected event resource to match")
	}
	if got.Details["foo"] != "bar" {
		t.Fatalf("expected details to be unmarshaled")
	}

	results, err := store.QueryEvents(ctx, AuditQueryFilters{
		EventType: string(APIAccess),
		Action:    "GET",
		Status:    string(StatusSuccess),
		Resource:  "/",
		Limit:     10,
		OrderBy:   "timestamp",
		OrderDesc: true,
	})
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected query results")
	}

	deleted, err := store.CleanupOldEvents(ctx, time.Hour)
	if err != nil {
		t.Fatalf("failed to cleanup events: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted event, got %d", deleted)
	}
}

func TestSQLiteAuditStoreAsyncAndRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.db")
	store, err := NewSQLiteAuditStoreWithRetention(path, 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	store.logger = NewLogger(WithOutput(&bytes.Buffer{}), WithLevel(LevelDebug))
	defer store.Close()

	event := NewAuditEvent(APIAccess, "async", StatusSuccess)
	event.ID = "event-async"
	store.SaveEventAsync(event)

	ctx := context.Background()
	count := 0
	for i := 0; i < 50; i++ {
		c, err := store.CountEvents(ctx, AuditQueryFilters{EventType: string(APIAccess)})
		if err != nil {
			t.Fatalf("failed to count events: %v", err)
		}
		count = c
		if c > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count == 0 {
		t.Fatalf("expected async event to be saved")
	}

	store.cleanupOldData()

	oldCh := store.eventChan
	store.eventChan = make(chan *AuditEvent, 1)
	close(oldCh)

	store.eventChan <- NewAuditEvent(APIAccess, "filled", StatusSuccess)
	store.SaveEventAsync(NewAuditEvent(APIAccess, "drop", StatusSuccess))
}

func TestNewSQLiteAuditStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.db")
	store, err := NewSQLiteAuditStore(path)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}
}
