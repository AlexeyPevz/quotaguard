package middleware

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/logging"
)

type captureStore struct {
	mu     sync.Mutex
	events []*logging.AuditEvent
}

func (c *captureStore) add(event *logging.AuditEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *captureStore) SaveEvent(event *logging.AuditEvent) error {
	c.add(event)
	return nil
}

func (c *captureStore) SaveEventAsync(event *logging.AuditEvent) {
	c.add(event)
}

func (c *captureStore) QueryEvents(ctx context.Context, filters logging.AuditQueryFilters) ([]*logging.AuditEvent, error) {
	return nil, nil
}

func (c *captureStore) GetEventByID(ctx context.Context, id string) (*logging.AuditEvent, error) {
	return nil, nil
}

func (c *captureStore) CountEvents(ctx context.Context, filters logging.AuditQueryFilters) (int, error) {
	return 0, nil
}

func (c *captureStore) CleanupOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func (c *captureStore) Close() error {
	return nil
}

func (c *captureStore) lastEvent() *logging.AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return nil
	}
	return c.events[len(c.events)-1]
}

func TestAuditMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &captureStore{}

	r := gin.New()
	r.Use(AuditMiddleware(store))

	r.GET("/ok", func(c *gin.Context) {
		c.Set("user_id", "user")
		c.Status(200)
	})
	r.GET("/fail", func(c *gin.Context) {
		c.Status(401)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ok?x=1", nil)
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/fail", nil)
	r.ServeHTTP(w, req)

	if len(store.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(store.events))
	}

	if store.events[0].EventType != logging.APIAccess {
		t.Fatalf("expected APIAccess event")
	}
	if store.events[0].UserID != "user" {
		t.Fatalf("expected user_id to be set")
	}
	if store.events[1].EventType != logging.AuthFailure {
		t.Fatalf("expected AuthFailure event")
	}
	if store.events[0].Details == nil {
		t.Fatalf("expected details to be set")
	}
}

func TestAuditEventAndResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &captureStore{}

	r := gin.New()
	r.Use(AuditEvent(store, logging.ConfigChange, "update"))
	r.GET("/cfg", func(c *gin.Context) {
		SetAuditResource(c, "resource-1")
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/cfg", nil)
	r.ServeHTTP(w, req)

	event := store.lastEvent()
	if event == nil || event.Resource != "resource-1" {
		t.Fatalf("expected audit resource")
	}
}

func TestAuditEventWithBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &captureStore{}

	r := gin.New()
	r.Use(AuditEventWithBody(store, logging.AdminAction, "post", true))
	r.POST("/body", func(c *gin.Context) {
		c.Status(200)
	})

	body := strings.Repeat("a", 1100)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/body", bytes.NewBufferString(body))
	r.ServeHTTP(w, req)

	event := store.lastEvent()
	if event == nil || event.Details == nil {
		t.Fatalf("expected event details")
	}
	if b, ok := event.Details["body"].(string); !ok || !strings.HasSuffix(b, "...") {
		t.Fatalf("expected truncated body")
	}
}

func TestAuditAuthSuccessFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &captureStore{}

	r := gin.New()
	r.Use(AuditAuthSuccess(store))
	r.GET("/auth", func(c *gin.Context) {
		c.Set("user_id", "u1")
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth", nil)
	r.ServeHTTP(w, req)
	if store.lastEvent() == nil || store.lastEvent().EventType != logging.AuthSuccess {
		t.Fatalf("expected auth success event")
	}

	store = &captureStore{}
	r = gin.New()
	r.Use(AuditAuthFailure(store))
	r.GET("/authfail", func(c *gin.Context) {
		c.Set("auth_error", "bad")
		c.Status(401)
	})

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/authfail", nil)
	r.ServeHTTP(w, req)
	if store.lastEvent() == nil || store.lastEvent().EventType != logging.AuthFailure {
		t.Fatalf("expected auth failure event")
	}
	if store.lastEvent().ErrorMessage != "bad" {
		t.Fatalf("expected auth error message")
	}
}

func TestAuditQuotaChangeReservationAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &captureStore{}

	r := gin.New()
	r.Use(AuditQuotaChange(store))
	r.PUT("/quota", func(c *gin.Context) {
		c.Set("account_id", "acc")
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/quota", nil)
	r.ServeHTTP(w, req)
	if store.lastEvent() == nil || store.lastEvent().Resource != "acc" {
		t.Fatalf("expected quota change resource")
	}

	store = &captureStore{}
	r = gin.New()
	r.Use(AuditReservation(store, "reserve"))
	r.POST("/reserve", func(c *gin.Context) {
		c.Set("reservation_id", "res")
		c.Status(200)
	})

	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/reserve", nil)
	r.ServeHTTP(w, req)
	if store.lastEvent() == nil || store.lastEvent().Resource != "res" {
		t.Fatalf("expected reservation resource")
	}

	store = &captureStore{}
	r = gin.New()
	r.Use(AuditReservation(store, "reserve"))
	r.POST("/reserve-fail", func(c *gin.Context) {
		c.Status(400)
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/reserve-fail", nil)
	r.ServeHTTP(w, req)
	if store.lastEvent() == nil || store.lastEvent().Status != logging.StatusFailure {
		t.Fatalf("expected reservation failure event")
	}

	store = &captureStore{}
	r = gin.New()
	r.Use(AuditAdminAction(store, "admin"))
	r.POST("/admin", func(c *gin.Context) {
		c.Status(500)
		c.Set("error_message", "oops")
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/admin", nil)
	r.ServeHTTP(w, req)
	if store.lastEvent() == nil || store.lastEvent().Status != logging.StatusFailure {
		t.Fatalf("expected admin failure event")
	}
	if store.lastEvent().ErrorMessage != "oops" {
		t.Fatalf("expected admin error message")
	}
}

func TestGetAuditStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	store := &captureStore{}
	ctx.Set("audit_store", store)

	got, ok := GetAuditStore(ctx)
	if !ok || got == nil {
		t.Fatalf("expected audit store")
	}
}
