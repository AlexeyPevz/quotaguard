package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockParserRegistry is a mock implementation for testing
type MockParserRegistry struct {
	parseFunc      func(provider models.Provider, headers http.Header, accountID string) (*models.QuotaInfo, error)
	autoDetectFunc func(headers http.Header, accountID string) (*models.QuotaInfo, models.Provider, error)
}

func (m *MockParserRegistry) Parse(provider models.Provider, headers http.Header, accountID string) (*models.QuotaInfo, error) {
	if m.parseFunc != nil {
		return m.parseFunc(provider, headers, accountID)
	}
	return &models.QuotaInfo{AccountID: accountID, Provider: provider}, nil
}

func (m *MockParserRegistry) AutoDetect(headers http.Header, accountID string) (*models.QuotaInfo, models.Provider, error) {
	if m.autoDetectFunc != nil {
		return m.autoDetectFunc(headers, accountID)
	}
	return &models.QuotaInfo{AccountID: accountID, Provider: models.ProviderOpenAI}, models.ProviderOpenAI, nil
}

func TestNewPassiveCollector(t *testing.T) {
	s := store.NewMemoryStore()
	c := NewPassiveCollector(s, 100, 2*time.Second)

	require.NotNil(t, c)
	assert.Equal(t, 100, c.bufferSize)
	assert.Equal(t, 2*time.Second, c.flushInt)
	assert.NotNil(t, c.buffer)
	assert.NotNil(t, c.store)
}

func TestPassiveCollector_StartStop(t *testing.T) {
	s := store.NewMemoryStore()
	ctx := context.Background()

	t.Run("start and stop", func(t *testing.T) {
		c := NewPassiveCollector(s, 100, 100*time.Millisecond)
		err := c.Start(ctx)
		require.NoError(t, err)
		assert.True(t, c.IsRunning())

		err = c.Stop()
		require.NoError(t, err)
		assert.False(t, c.IsRunning())
	})

	t.Run("double start", func(t *testing.T) {
		c := NewPassiveCollector(s, 100, 100*time.Millisecond)
		err := c.Start(ctx)
		require.NoError(t, err)

		err = c.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		if err := c.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	})

	t.Run("stop when not running", func(t *testing.T) {
		c := NewPassiveCollector(s, 100, 100*time.Millisecond)
		err := c.Stop()
		require.NoError(t, err)
	})
}

func TestPassiveCollector_Ingest(t *testing.T) {
	s := store.NewMemoryStore()
	c := NewPassiveCollector(s, 10, 100*time.Millisecond)

	ctx := context.Background()
	err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if err := c.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	}()

	t.Run("successful ingest", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID: "acc-1",
			Provider:  models.ProviderOpenAI,
		}

		err := c.Ingest(quota)
		require.NoError(t, err)
		assert.Equal(t, 1, c.BufferSize())
	})

	t.Run("buffer full", func(t *testing.T) {
		// Fill the buffer
		toFill := c.bufferSize - c.BufferSize()
		for i := 0; i < toFill; i++ {
			quota := &models.QuotaInfo{
				AccountID: string(rune('a' + i)),
				Provider:  models.ProviderOpenAI,
			}
			require.NoError(t, c.Ingest(quota))
		}

		// This should fail
		quota := &models.QuotaInfo{
			AccountID: "overflow",
			Provider:  models.ProviderOpenAI,
		}
		err := c.Ingest(quota)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer full")
	})
}

func TestPassiveCollector_Flush(t *testing.T) {
	s := store.NewMemoryStore()
	c := NewPassiveCollector(s, 100, 50*time.Millisecond)

	ctx := context.Background()
	err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if err := c.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	}()

	// Ingest some quotas
	for i := 0; i < 5; i++ {
		quota := &models.QuotaInfo{
			AccountID: string(rune('a' + i)),
			Provider:  models.ProviderOpenAI,
		}
		err := c.Ingest(quota)
		require.NoError(t, err)
	}

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	// Check store
	for i := 0; i < 5; i++ {
		_, ok := s.GetQuota(string(rune('a' + i)))
		assert.True(t, ok, "quota for account %c should be in store", rune('a'+i))
	}
}

func TestPassiveCollector_HTTPHandler(t *testing.T) {
	s := store.NewMemoryStore()
	c := NewPassiveCollector(s, 100, 100*time.Millisecond)

	ctx := context.Background()
	err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if err := c.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	}()

	registry := &MockParserRegistry{
		parseFunc: func(provider models.Provider, headers http.Header, accountID string) (*models.QuotaInfo, error) {
			return &models.QuotaInfo{
				AccountID: accountID,
				Provider:  provider,
			}, nil
		},
	}

	handler := c.HTTPHandler(registry)

	t.Run("successful ingest", func(t *testing.T) {
		req := IngestRequest{
			AccountID: "test-acc",
			Provider:  models.ProviderOpenAI,
			Headers: map[string][]string{
				"X-Ratelimit-Limit-Requests": []string{"1000"},
			},
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler(w, r)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp IngestResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("method not allowed", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ingest", nil)
		w := httptest.NewRecorder()

		handler(w, r)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader([]byte("invalid")))
		w := httptest.NewRecorder()

		handler(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing account_id", func(t *testing.T) {
		req := IngestRequest{
			Provider: models.ProviderOpenAI,
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("parse error", func(t *testing.T) {
		errorRegistry := &MockParserRegistry{
			parseFunc: func(provider models.Provider, headers http.Header, accountID string) (*models.QuotaInfo, error) {
				return nil, assert.AnError
			},
		}

		errorHandler := c.HTTPHandler(errorRegistry)

		req := IngestRequest{
			AccountID: "test-acc",
			Provider:  models.ProviderOpenAI,
			Headers:   map[string][]string{},
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		errorHandler(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestPassiveCollector_HTTPHandlerWithAutoDetect(t *testing.T) {
	s := store.NewMemoryStore()
	c := NewPassiveCollector(s, 100, 100*time.Millisecond)

	ctx := context.Background()
	err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if err := c.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	}()

	registry := &MockParserRegistry{
		autoDetectFunc: func(headers http.Header, accountID string) (*models.QuotaInfo, models.Provider, error) {
			return &models.QuotaInfo{
				AccountID: accountID,
				Provider:  models.ProviderOpenAI,
			}, models.ProviderOpenAI, nil
		},
	}

	handler := c.HTTPHandlerWithAutoDetect(registry)

	t.Run("successful auto-detect", func(t *testing.T) {
		req := IngestRequest{
			AccountID: "test-acc",
			Headers: map[string][]string{
				"X-Ratelimit-Limit-Requests": []string{"1000"},
			},
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		handler(w, r)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp IngestResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("auto-detect error", func(t *testing.T) {
		errorRegistry := &MockParserRegistry{
			autoDetectFunc: func(headers http.Header, accountID string) (*models.QuotaInfo, models.Provider, error) {
				return nil, "", assert.AnError
			},
		}

		errorHandler := c.HTTPHandlerWithAutoDetect(errorRegistry)

		req := IngestRequest{
			AccountID: "test-acc",
			Headers:   map[string][]string{},
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		errorHandler(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestPassiveCollector_BufferSize(t *testing.T) {
	s := store.NewMemoryStore()
	c := NewPassiveCollector(s, 100, 100*time.Millisecond)

	assert.Equal(t, 0, c.BufferSize())

	ctx := context.Background()
	err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if err := c.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	}()

	quota := &models.QuotaInfo{
		AccountID: "test",
		Provider:  models.ProviderOpenAI,
	}

	err = c.Ingest(quota)
	require.NoError(t, err)

	assert.Equal(t, 1, c.BufferSize())
}
