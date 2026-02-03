package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

func TestProviderClientEndpoints(t *testing.T) {
	client := NewProviderClient(2 * time.Second)
	client.AddEndpoint(models.ProviderOpenAI, "http://example.com")

	if endpoint, ok := client.GetEndpoint(models.ProviderOpenAI); !ok || endpoint == "" {
		t.Fatalf("expected endpoint to be set")
	}

	client.SetEndpoints(map[models.Provider]string{models.ProviderAnthropic: "http://anthropic"})
	if endpoint, ok := client.GetEndpoint(models.ProviderAnthropic); !ok || endpoint == "" {
		t.Fatalf("expected anthropic endpoint")
	}

	defaults := DefaultProviderEndpoints()
	if defaults[models.ProviderOpenAI] == "" {
		t.Fatalf("expected default openai endpoint")
	}
}

func TestProviderClientCheckHealth(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okServer.Close()

	unauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer unauthServer.Close()

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()

	client := NewProviderClient(2 * time.Second)
	client.AddEndpoint(models.ProviderOpenAI, okServer.URL)
	client.AddEndpoint(models.ProviderAnthropic, unauthServer.URL)
	client.AddEndpoint(models.ProviderGemini, badServer.URL)

	ctx := context.Background()
	result, err := client.CheckHealth(ctx, models.ProviderOpenAI)
	if err != nil || result == nil || !result.Available {
		t.Fatalf("expected openai to be available")
	}

	result, err = client.CheckHealth(ctx, models.ProviderAnthropic)
	if err != nil || result == nil || !result.Available {
		t.Fatalf("expected anthropic to be available despite 401")
	}

	result, err = client.CheckHealth(ctx, models.ProviderGemini)
	if err != nil || result == nil || result.Available {
		t.Fatalf("expected gemini to be unavailable")
	}

	missing, err := client.CheckHealth(ctx, models.ProviderAzure)
	if err != nil || missing == nil || missing.Available {
		t.Fatalf("expected missing endpoint to be unavailable")
	}

	all := client.CheckAll(ctx)
	if len(all) != 3 {
		t.Fatalf("expected 3 results, got %d", len(all))
	}
}

func TestProviderClientHeaders(t *testing.T) {
	client := NewProviderClient(time.Second)

	req, _ := http.NewRequest(http.MethodHead, "http://example.com", nil)
	client.addProviderHeaders(req, models.ProviderAnthropic)
	if req.Header.Get("x-api-key") == "" {
		t.Fatalf("expected anthropic header")
	}

	req, _ = http.NewRequest(http.MethodHead, "http://example.com", nil)
	client.addProviderHeaders(req, models.ProviderOpenAI)
	if req.Header.Get("Authorization") == "" {
		t.Fatalf("expected openai auth header")
	}

	req, _ = http.NewRequest(http.MethodHead, "http://example.com", nil)
	client.addProviderHeaders(req, models.ProviderGemini)
	if req.Header.Get("Content-Type") == "" {
		t.Fatalf("expected gemini content type header")
	}

	req, _ = http.NewRequest(http.MethodHead, "http://example.com", nil)
	client.addProviderHeaders(req, models.ProviderAzure)
	if req.Header.Get("api-key") == "" {
		t.Fatalf("expected azure api-key header")
	}
}
