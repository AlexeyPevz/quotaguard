package mocks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/stretchr/testify/require"
)

func TestMockHTTPClient(t *testing.T) {
	client := NewMockHTTPClient()
	resp := &MockResponse{StatusCode: 200, Body: map[string]interface{}{"ok": true}}
	client.SetResponse("https://api.example.com/test", resp)
	client.RequestDelay = time.Millisecond

	req, err := http.NewRequest("POST", "https://api.example.com/test", bytes.NewBufferString("hello"))
	require.NoError(t, err)

	res, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, res.StatusCode)

	var decoded map[string]interface{}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&decoded))
	require.Equal(t, true, decoded["ok"])

	reqs := client.GetRequests()
	require.Len(t, reqs, 1)
	require.Equal(t, "POST", reqs[0].Method)

	client.ClearRequests()
	require.Len(t, client.GetRequests(), 0)

	// wildcard match
	client.SetResponse("*", &MockResponse{StatusCode: 201, Body: map[string]interface{}{"ok": "wild"}})
	req, _ = http.NewRequest("GET", "https://api.example.com/other", nil)
	res, err = client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 201, res.StatusCode)
}

func TestMockResponses(t *testing.T) {
	openai := MockQuotaResponse(models.ProviderOpenAI)
	require.Equal(t, 200, openai.StatusCode)
	anthropic := MockQuotaResponse(models.ProviderAnthropic)
	require.Equal(t, 200, anthropic.StatusCode)
	other := MockQuotaResponse("unknown")
	require.Equal(t, 200, other.StatusCode)

	errResp := MockErrorResponse(500, "fail")
	require.Equal(t, 500, errResp.StatusCode)

	rateResp := MockRateLimitResponse()
	require.Equal(t, 429, rateResp.StatusCode)
}

func TestMockExternalAPI(t *testing.T) {
	api := NewMockExternalAPI()
	api.RegisterHandler("/ok", func(req *APIRequest) (*APIResponse, error) {
		return &APIResponse{StatusCode: 200, Body: map[string]interface{}{"ok": true}}, nil
	})

	res, err := api.HandleRequest(context.Background(), &APIRequest{Method: "GET", URL: "/ok"})
	require.NoError(t, err)
	require.Equal(t, 200, res.StatusCode)
	require.Equal(t, 1, api.GetRequestCount())
	require.Greater(t, api.AverageLatency, time.Duration(0))

	res, err = api.HandleRequest(context.Background(), &APIRequest{Method: "GET", URL: "/missing"})
	require.NoError(t, err)
	require.Equal(t, 404, res.StatusCode)

	api.ResetRequestCount()
	require.Equal(t, 0, api.GetRequestCount())
}

func TestQuotaInfoFromResponse(t *testing.T) {
	openaiResp := MockQuotaResponse(models.ProviderOpenAI)
	openaiBody, _ := json.Marshal(openaiResp.Body)
	quota, err := QuotaInfoFromResponse(models.ProviderOpenAI, openaiBody)
	require.NoError(t, err)
	require.Len(t, quota.Dimensions, 2)

	anthropicResp := MockQuotaResponse(models.ProviderAnthropic)
	anthropicBody, _ := json.Marshal(anthropicResp.Body)
	quota, err = QuotaInfoFromResponse(models.ProviderAnthropic, anthropicBody)
	require.NoError(t, err)
	require.Len(t, quota.Dimensions, 1)

	_, err = QuotaInfoFromResponse(models.ProviderOpenAI, []byte("{invalid"))
	require.Error(t, err)

	_, _ = OpenAIHandler()(nil)
	_, _ = AnthropicHandler()(nil)
	_, _ = RateLimitedHandler()(nil)
	_, _ = ErrorHandler(500, "boom")(nil)

	SimulateNetworkDelay(0)
}

func TestMockTelegramBotAndHandler(t *testing.T) {
	bot := NewMockTelegramBot()
	require.NoError(t, bot.SendMessage(1, "hi", "Markdown"))
	require.Equal(t, 1, bot.GetSentCount())

	alert := &MockAlert{ID: "a", Title: "t", Message: "m", AccountID: "acc", Severity: SeverityCritical, ChatID: 2, CreatedAt: time.Now()}
	require.NoError(t, bot.SendAlert(alert))
	require.NoError(t, bot.SendHealthNotification("healthy", "all good"))

	messages := bot.GetSentMessages()
	require.NotEmpty(t, messages)

	bot.ClearSentMessages()
	require.Equal(t, 0, bot.GetSentCount())

	bot.AddError(errors.New("boom"))
	require.Len(t, bot.Errors, 1)

	handler := NewMockTelegramHandler()
	resp := handler.ProcessUpdate(TelegramUpdate{Message: TelegramMessage{Text: "/status"}})
	require.Contains(t, resp, "System")
	_ = handler.ProcessUpdate(TelegramUpdate{Message: TelegramMessage{Text: "/health"}})
	_ = handler.ProcessUpdate(TelegramUpdate{Message: TelegramMessage{Text: "/start"}})
	_ = handler.ProcessUpdate(TelegramUpdate{Message: TelegramMessage{Text: "/help"}})
	_ = handler.ProcessUpdate(TelegramUpdate{Message: TelegramMessage{Text: "/unknown"}})
	require.Len(t, handler.GetUpdates(), 5)
	require.Len(t, handler.GetResponses(), 5)
}

func TestMockRouter(t *testing.T) {
	mock := NewMockRouter()
	acc := &models.Account{ID: "acc", Provider: models.ProviderOpenAI, Enabled: true, Tier: "free"}
	mock.SetAccounts([]*models.Account{acc})
	mock.SetQuotas(map[string]*models.QuotaInfo{
		"acc": {AccountID: "acc", Provider: models.ProviderOpenAI, EffectiveRemainingPct: 50.0},
	})

	resp, err := mock.Select(context.Background(), router.SelectRequest{})
	require.NoError(t, err)
	require.Equal(t, "mock-account", resp.AccountID)

	mock.SetSelectResponse(&router.SelectResponse{AccountID: "fixed"})
	resp, err = mock.Select(context.Background(), router.SelectRequest{})
	require.NoError(t, err)
	require.Equal(t, "fixed", resp.AccountID)

	mock.SetSelectError(errors.New("fail"))
	_, err = mock.Select(context.Background(), router.SelectRequest{})
	require.Error(t, err)

	require.NoError(t, mock.Feedback(context.Background(), &router.FeedbackRequest{AccountID: "acc"}))
	mock.AssertFeedbackCalled(t)

	accounts, err := mock.GetAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)

	quota, err := mock.GetQuota(context.Background(), "acc")
	require.NoError(t, err)
	require.NotNil(t, quota)

	_, err = mock.GetAllQuotas(context.Background())
	require.NoError(t, err)

	_, err = mock.GetRoutingDistribution(context.Background())
	require.NoError(t, err)

	status, err := mock.CheckHealth(context.Background(), "acc")
	require.NoError(t, err)
	require.Equal(t, "acc", status.AccountID)

	mock.SetCurrentAccount("acc")
	mock.RecordSwitch("acc")
	require.Equal(t, "acc", mock.GetCurrentAccount())

	cfg := &router.Config{}
	mock.SetConfig(cfg)
	require.Equal(t, cfg, mock.GetConfig())

	accStatus, err := mock.GetAccountStatus("acc")
	require.NoError(t, err)
	require.Equal(t, "acc", accStatus.AccountID)
	require.True(t, accStatus.HasQuotaData)

	accStatus, err = mock.GetAccountStatus("missing")
	require.NoError(t, err)
	require.Equal(t, "missing", accStatus.AccountID)

	_ = mock.CalculateOptimalDistribution(context.Background(), 10)

	mock.SetHealthy(false)
	require.False(t, mock.IsHealthy())

	require.NoError(t, mock.Close())
	mock.AssertCloseCalled(t)
}
