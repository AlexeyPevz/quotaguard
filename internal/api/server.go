package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/collector"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/errors"
	"github.com/quotaguard/quotaguard/internal/logging"
	"github.com/quotaguard/quotaguard/internal/metrics"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/reservation"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
)

// Server represents the HTTP API server
type Server struct {
	router      *gin.Engine
	config      config.ServerConfig
	apiConfig   config.APIConfig
	store       store.Store
	routerSvc   router.Router
	reservation *reservation.Manager
	collector   *collector.PassiveCollector
	metrics     *metrics.Metrics
	logger      *logging.Logger
	rateLimiter *IPRateLimiter
	httpServer  *http.Server
	tlsConfig   config.TLSConfig
}

// Router returns the gin router for testing purposes
func (s *Server) Router() *gin.Engine {
	return s.router
}

// NewServer creates a new API server
func NewServer(cfg config.ServerConfig, apiCfg config.APIConfig, s store.Store, r router.Router, rm *reservation.Manager, c *collector.PassiveCollector) *Server {
	gin.SetMode(gin.ReleaseMode)

	// Initialize metrics and logger
	m := metrics.NewMetrics("quotaguard")
	logger := logging.NewLogger()

	// Initialize rate limiter from config with sane defaults
	requestsPerMinute := apiCfg.RateLimit.RequestsPerMinute
	if requestsPerMinute <= 0 {
		requestsPerMinute = 1000
	}
	burst := apiCfg.RateLimit.Burst
	if burst <= 0 {
		burst = 100
	}
	rateLimiter := newIPRateLimiter(time.Minute/time.Duration(requestsPerMinute), burst)

	server := &Server{
		router:      gin.New(),
		config:      cfg,
		apiConfig:   apiCfg,
		store:       s,
		routerSvc:   r,
		reservation: rm,
		collector:   c,
		metrics:     m,
		logger:      logger,
		rateLimiter: rateLimiter,
		tlsConfig:   cfg.TLS,
	}
	server.router.HandleMethodNotAllowed = true

	// Add recovery middleware with logging
	server.router.Use(gin.Recovery())

	// Add rate limiting middleware
	server.router.Use(rateLimitMiddleware(rateLimiter))

	// Add body size limit (1MB)
	server.router.Use(bodyLimitMiddleware(1 << 20))

	// Add metrics and logging middleware
	server.router.Use(metrics.Middleware(m, logger))

	// Add logging middleware for structured logs
	server.router.Use(loggingMiddleware(logger))

	server.setupRoutes()
	return server
}

// loggingMiddleware provides structured logging for all requests
func loggingMiddleware(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Get or generate correlation ID
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			correlationID = logging.GenerateCorrelationID()
		}

		// Add to context
		ctx := logging.WithCorrelationID(c.Request.Context(), correlationID)
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Log request completion
		duration := time.Since(start).Seconds()
		logger.InfoWithContext(ctx, "request completed",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_seconds", duration,
		)
	}
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// Prometheus metrics endpoint - NO authentication required
	s.router.GET("/metrics", gin.WrapH(s.metrics.Handler()))

	// Health check - NO authentication required
	s.router.GET("/health", s.handleHealth)

	// Create auth middleware based on configuration
	authMiddleware := APIKeyAuth(s.apiConfig.Auth.APIKeys, s.apiConfig.Auth.HeaderName, s.logger)

	// Router endpoints - require authentication
	routerGroup := s.router.Group("")
	routerGroup.Use(authMiddleware)
	{
		routerGroup.POST("/router/select", s.handleRouterSelect)
		routerGroup.POST("/router/feedback", s.handleRouterFeedback)
		routerGroup.GET("/router/distribution", s.handleRouterDistribution)
	}

	// Quota endpoints - require authentication
	quotaGroup := s.router.Group("")
	quotaGroup.Use(authMiddleware)
	{
		quotaGroup.GET("/quotas", s.handleListQuotas)
		quotaGroup.GET("/quotas/:account_id", s.handleGetQuota)
	}

	// Reservation endpoints - require authentication
	reservationGroup := s.router.Group("")
	reservationGroup.Use(authMiddleware)
	{
		reservationGroup.POST("/reservations", s.handleCreateReservation)
		reservationGroup.POST("/reservations/:id/release", s.handleReleaseReservation)
		reservationGroup.POST("/reservations/:id/cancel", s.handleCancelReservation)
		reservationGroup.GET("/reservations/:id", s.handleGetReservation)
	}

	// Ingest endpoint - require authentication
	ingestGroup := s.router.Group("")
	ingestGroup.Use(authMiddleware)
	{
		ingestGroup.POST("/ingest", s.handleIngest)
	}
}

// Run starts the HTTP or HTTPS server based on TLS configuration
func (s *Server) Run() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.HTTPPort)

	if s.tlsConfig.Enabled {
		return s.RunTLS()
	}

	if err := s.startCollector(); err != nil {
		return err
	}

	// Create http server if not already created
	if s.httpServer == nil {
		s.httpServer = NewHTTPServer(addr, s.router)
	}

	s.logger.Info("starting HTTP server", "addr", addr)
	return s.httpServer.ListenAndServe()
}

// RunTLS starts the HTTPS server with TLS configuration
func (s *Server) RunTLS() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.HTTPPort)

	s.logger.Info("starting HTTPS server", "addr", addr, "cert_file", s.tlsConfig.CertFile, "min_version", s.tlsConfig.MinVersion)

	if err := s.startCollector(); err != nil {
		return err
	}

	// Create HTTPS server
	srv, err := NewHTTPSServerWithConfig(addr, s.tlsConfig.CertFile, s.tlsConfig.KeyFile, s.tlsConfig.MinVersion, s.router)
	if err != nil {
		return &errors.ErrServerStart{Addr: addr, Err: err}
	}
	s.httpServer = srv

	return s.httpServer.ListenAndServe()
}

// StartWithServer starts the server with a pre-configured http.Server
func (s *Server) StartWithServer(srv *http.Server) error {
	if err := s.startCollector(); err != nil {
		return err
	}
	s.httpServer = srv
	s.logger.Info("starting HTTP server", "addr", srv.Addr)
	return srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("initiating graceful shutdown")

	var wg sync.WaitGroup
	errs := make(chan error, 5)

	// Stop accepting new connections
	if s.httpServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.logger.Info("shutting down HTTP server")
			if err := s.httpServer.Shutdown(ctx); err != nil {
				s.logger.Error("HTTP server shutdown error", "error", err.Error())
				errs <- &errors.ErrServerShutdown{Err: err}
			}
		}()
	}

	// Release active reservations
	if s.reservation != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.reservation.ReleaseAll(ctx); err != nil {
				errs <- fmt.Errorf("release reservations: %w", err)
			}
		}()
	}

	// Stop collector (flushes pending updates)
	if s.collector != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.collector.Stop(); err != nil {
				errs <- fmt.Errorf("collector stop: %w", err)
			}
		}()
	}

	// Close router
	if s.routerSvc != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.routerSvc.Close(); err != nil {
				errs <- fmt.Errorf("router close: %w", err)
			}
		}()
	}

	// Close store connections
	if s.store != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.store.Close(); err != nil {
				errs <- fmt.Errorf("store close: %w", err)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	close(errs)
	var errList []error
	for err := range errs {
		if err != nil {
			errList = append(errList, err)
		}
	}
	if len(errList) > 0 {
		return fmt.Errorf("shutdown errors: %v", errList)
	}

	s.logger.Info("graceful shutdown completed")
	return nil
}

// handleHealth returns health status
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"router":    s.routerSvc.IsHealthy(),
	})
}

// RouterSelectRequest represents a request to select an account
type RouterSelectRequest struct {
	Provider         string   `json:"provider,omitempty"`
	RequiredDims     []string `json:"required_dimensions,omitempty"`
	EstimatedCost    float64  `json:"estimated_cost_percent,omitempty"`
	EstimatedTokens  int64    `json:"estimated_tokens,omitempty"`
	Policy           string   `json:"policy,omitempty"`
	Exclude          []string `json:"exclude_accounts,omitempty"`
	ExcludeProviders []string `json:"exclude_providers,omitempty"`
}

// RouterSelectResponse represents the response from select
type RouterSelectResponse struct {
	AccountID      string   `json:"account_id"`
	Provider       string   `json:"provider"`
	Score          float64  `json:"score"`
	Reason         string   `json:"reason"`
	AlternativeIDs []string `json:"alternative_ids,omitempty"`
}

// handleRouterSelect handles account selection requests
func (s *Server) handleRouterSelect(c *gin.Context) {
	var req RouterSelectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert request to router.SelectRequest
	routerReq := router.SelectRequest{
		EstimatedCost:   req.EstimatedCost,
		EstimatedTokens: req.EstimatedTokens,
		Policy:          req.Policy,
		Exclude:         req.Exclude,
	}

	// Convert provider
	if req.Provider != "" {
		routerReq.Provider = models.Provider(req.Provider)
	}

	// Convert excluded providers
	for _, p := range req.ExcludeProviders {
		routerReq.ExcludeProviders = append(routerReq.ExcludeProviders, models.Provider(p))
	}

	// Convert required dimensions
	for _, d := range req.RequiredDims {
		routerReq.RequiredDims = append(routerReq.RequiredDims, models.DimensionType(d))
	}

	resp, err := s.routerSvc.Select(c.Request.Context(), routerReq)
	if err != nil {
		s.logger.ErrorWithContext(c.Request.Context(), "router select failed",
			"error", err.Error(),
		)
		s.metrics.RecordError("router_error", "/router/select", "POST")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	// Record the switch
	s.routerSvc.RecordSwitch(resp.AccountID)

	// Record router decision
	s.metrics.RecordRouterDecision(req.Policy, "selected", string(resp.Provider))

	c.JSON(http.StatusOK, RouterSelectResponse{
		AccountID:      resp.AccountID,
		Provider:       string(resp.Provider),
		Score:          resp.Score,
		Reason:         resp.Reason,
		AlternativeIDs: resp.AlternativeIDs,
	})
}

// RouterFeedbackRequest represents feedback about routing
type RouterFeedbackRequest struct {
	AccountID     string  `json:"account_id" binding:"required"`
	ReservationID string  `json:"reservation_id,omitempty"`
	ActualCost    float64 `json:"actual_cost_percent,omitempty"`
	Success       bool    `json:"success"`
	Error         string  `json:"error,omitempty"`
}

// handleRouterFeedback handles routing feedback
func (s *Server) handleRouterFeedback(c *gin.Context) {
	var req RouterFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Release reservation if provided
	if req.ReservationID != "" && req.ActualCost > 0 {
		if err := s.reservation.Release(req.ReservationID, req.ActualCost); err != nil {
			s.logger.ErrorWithContext(c.Request.Context(), "failed to release reservation",
				"reservation_id", req.ReservationID,
				"error", err.Error(),
			)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "feedback recorded"})
}

// handleRouterDistribution returns optimal distribution
func (s *Server) handleRouterDistribution(c *gin.Context) {
	distribution := s.routerSvc.CalculateOptimalDistribution(c.Request.Context(), 100)
	c.JSON(http.StatusOK, distribution)
}

// handleListQuotas returns all quotas
func (s *Server) handleListQuotas(c *gin.Context) {
	quotas := s.store.ListQuotas()
	if len(quotas) == 0 {
		c.JSON(http.StatusOK, []models.QuotaInfo{})
		return
	}

	ids := make([]string, 0, len(quotas))
	for id := range quotas {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	resp := make([]models.QuotaInfo, 0, len(quotas))
	for _, id := range ids {
		quota := quotas[id]
		if quota == nil {
			continue
		}
		resp = append(resp, *normalizeQuotaForResponse(quota))
	}

	c.JSON(http.StatusOK, resp)
}

// handleGetQuota returns quota for a specific account
func (s *Server) handleGetQuota(c *gin.Context) {
	accountID := c.Param("account_id")
	quota, ok := s.store.GetQuota(accountID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "quota not found"})
		return
	}
	c.JSON(http.StatusOK, normalizeQuotaForResponse(quota))
}

func (s *Server) startCollector() error {
	if s.collector == nil {
		return nil
	}
	if s.collector.IsRunning() {
		return nil
	}
	return s.collector.Start(context.Background())
}

func normalizeQuotaForResponse(quota *models.QuotaInfo) *models.QuotaInfo {
	if quota == nil {
		return nil
	}
	normalized := *quota
	normalized.EffectiveRemainingPct = quota.EffectiveRemainingWithVirtual()
	return &normalized
}

// CreateReservationRequest represents a request to create a reservation
type CreateReservationRequest struct {
	AccountID        string  `json:"account_id" binding:"required"`
	EstimatedCostPct float64 `json:"estimated_cost_percent" binding:"required,min=0,max=100"`
	CorrelationID    string  `json:"correlation_id" binding:"required"`
}

// CreateReservationResponse represents the response from create reservation
type CreateReservationResponse struct {
	ReservationID string    `json:"reservation_id"`
	AccountID     string    `json:"account_id"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// handleCreateReservation creates a new reservation
func (s *Server) handleCreateReservation(c *gin.Context) {
	var req CreateReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	res, err := s.reservation.Create(c.Request.Context(), req.AccountID, req.EstimatedCostPct, req.CorrelationID)
	if err != nil {
		s.logger.ErrorWithContext(c.Request.Context(), "failed to create reservation",
			"account_id", req.AccountID,
			"error", err.Error(),
		)
		s.metrics.RecordReservation("create", "error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	s.metrics.RecordReservation("create", "success")

	s.logger.InfoWithContext(c.Request.Context(), "reservation created",
		"reservation_id", res.ID,
		"account_id", res.AccountID,
	)

	c.JSON(http.StatusCreated, CreateReservationResponse{
		ReservationID: res.ID,
		AccountID:     res.AccountID,
		Status:        string(res.Status),
		ExpiresAt:     res.ExpiresAt,
	})
}

// handleReleaseReservation releases a reservation
func (s *Server) handleReleaseReservation(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		ActualCostPct float64 `json:"actual_cost_percent" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.reservation.Release(id, req.ActualCostPct); err != nil {
		s.logger.ErrorWithContext(c.Request.Context(), "failed to release reservation",
			"reservation_id", id,
			"error", err.Error(),
		)
		s.metrics.RecordReservation("release", "error")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.metrics.RecordReservation("release", "success")

	s.logger.InfoWithContext(c.Request.Context(), "reservation released",
		"reservation_id", id,
	)

	c.JSON(http.StatusOK, gin.H{"status": "released"})
}

// handleCancelReservation cancels a reservation
func (s *Server) handleCancelReservation(c *gin.Context) {
	id := c.Param("id")

	if err := s.reservation.Cancel(id); err != nil {
		s.logger.ErrorWithContext(c.Request.Context(), "failed to cancel reservation",
			"reservation_id", id,
			"error", err.Error(),
		)
		s.metrics.RecordReservation("cancel", "error")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.metrics.RecordReservation("cancel", "success")

	s.logger.InfoWithContext(c.Request.Context(), "reservation cancelled",
		"reservation_id", id,
	)

	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// handleGetReservation returns a reservation
func (s *Server) handleGetReservation(c *gin.Context) {
	id := c.Param("id")

	res, ok := s.reservation.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "reservation not found"})
		return
	}
	c.JSON(http.StatusOK, res)
}

// IngestRequest represents a quota update from external source
type IngestRequest struct {
	AccountID             string             `json:"account_id" binding:"required"`
	Provider              string             `json:"provider" binding:"required"`
	EffectiveRemainingPct float64            `json:"effective_remaining_percent"`
	Dimensions            []models.Dimension `json:"dimensions,omitempty"`
	IsThrottled           bool               `json:"is_throttled"`
	Source                string             `json:"source"`
}

// handleIngest handles quota updates from external sources
func (s *Server) handleIngest(c *gin.Context) {
	var req IngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	quota := &models.QuotaInfo{
		AccountID:             req.AccountID,
		Provider:              models.Provider(req.Provider),
		EffectiveRemainingPct: req.EffectiveRemainingPct,
		Dimensions:            req.Dimensions,
		IsThrottled:           req.IsThrottled,
		Source:                models.Source(req.Source),
		CollectedAt:           time.Now(),
	}

	// Update store
	s.store.SetQuota(req.AccountID, quota)

	// Record quota utilization
	s.metrics.RecordQuotaUtilization(req.AccountID, req.Provider, "total", req.EffectiveRemainingPct)

	// Also send to passive collector if available
	if s.collector != nil {
		if err := s.collector.Ingest(quota); err != nil {
			s.metrics.RecordCollector("ingest", "error", req.Source)
			s.logger.ErrorWithContext(c.Request.Context(), "quota ingest failed", "error", err.Error())
		} else {
			s.metrics.RecordCollector("ingest", "success", req.Source)
		}
	} else {
		s.metrics.RecordCollector("ingest", "success", "direct")
	}

	s.logger.InfoWithContext(c.Request.Context(), "quota ingested",
		"account_id", req.AccountID,
		"provider", req.Provider,
		"remaining_percent", req.EffectiveRemainingPct,
	)

	c.JSON(http.StatusOK, gin.H{"status": "ingested"})
}
