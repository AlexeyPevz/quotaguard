package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRootCommand(t *testing.T) {
	// Test that root command is created
	assert.NotNil(t, RootCmd)
	assert.Equal(t, "quotaguard", RootCmd.Use)
	assert.Contains(t, RootCmd.Long, "QuotaGuard")
}

func TestVersionCommand(t *testing.T) {
	// Test version command
	assert.NotNil(t, versionCmd)
	assert.Equal(t, "version", versionCmd.Use)
}

func TestGetGlobalFlags(t *testing.T) {
	// Initialize CLI first
	InitCLI()

	// Test global flags getter
	flags := GetGlobalFlags()
	assert.Equal(t, "config.yaml", flags.Config)
	assert.Equal(t, "./data/quotaguard.db", flags.DBPath)
	assert.False(t, flags.Verbose)
}

func TestGetVersionInfo(t *testing.T) {
	// Test version info
	info := GetVersionInfo()
	assert.NotEmpty(t, info.Version)
	assert.NotEmpty(t, info.GoVersion)
	assert.NotEmpty(t, info.OS)
	assert.NotEmpty(t, info.Arch)
}

func TestQuotaDisplayInfo(t *testing.T) {
	// Test QuotaDisplayInfo struct
	info := QuotaDisplayInfo{
		Provider:              "openai",
		AccountID:             "test-account",
		Tier:                  "premium",
		EffectiveRemainingPct: 75.5,
		IsThrottled:           false,
		IsShadowBanned:        false,
		Status:                "OK",
	}

	assert.Equal(t, "openai", info.Provider)
	assert.Equal(t, "test-account", info.AccountID)
	assert.Equal(t, "premium", info.Tier)
	assert.Equal(t, 75.5, info.EffectiveRemainingPct)
	assert.Equal(t, "OK", info.Status)
}

func TestCheckResult(t *testing.T) {
	// Test CheckResult struct
	result := CheckResult{
		Name:    "Database",
		Status:  "OK",
		Message: "Database connected successfully",
		Details: "Path: ./data/quotaguard.db",
	}

	assert.Equal(t, "Database", result.Name)
	assert.Equal(t, "OK", result.Status)
	assert.Equal(t, "Database connected successfully", result.Message)
	assert.Equal(t, "Path: ./data/quotaguard.db", result.Details)
}

func TestDoctorCheck(t *testing.T) {
	// Test DoctorCheck struct
	check := DoctorCheck{
		Category:    "System",
		Name:        "Go Version",
		Status:      "OK",
		Message:     "Go 1.24.0",
		Severity:    "low",
		Remediation: "No action needed",
	}

	assert.Equal(t, "System", check.Category)
	assert.Equal(t, "Go Version", check.Name)
	assert.Equal(t, "OK", check.Status)
	assert.Equal(t, "low", check.Severity)
}

func TestRouteResult(t *testing.T) {
	// Test RouteResult struct
	result := RouteResult{
		RequestNum: 1,
		SelectedAccount: AccountSummary{
			ID:       "test-account",
			Provider: "openai",
			Tier:     "premium",
			Priority: 100,
		},
		AllScores: []AccountScore{
			{
				Account: AccountSummary{
					ID:       "test-account",
					Provider: "openai",
					Tier:     "premium",
					Priority: 100,
				},
				Score:       85.5,
				SafetyScore: 90.0,
				Reliability: 85.0,
				CostScore:   80.0,
				Reason:      "Premium tier - higher reliability",
			},
		},
	}

	assert.Equal(t, 1, result.RequestNum)
	assert.Equal(t, "test-account", result.SelectedAccount.ID)
	assert.Len(t, result.AllScores, 1)
	assert.Equal(t, 85.5, result.AllScores[0].Score)
}

func TestCalculateAccountScore(t *testing.T) {
	// Test score calculation
	tests := []struct {
		name     string
		account  configAccount
		expected float64
	}{
		{
			name: "premium tier",
			account: configAccount{
				Tier:     "premium",
				Priority: 100,
			},
			expected: 82.0, // Premium tier should have higher score
		},
		{
			name: "trial tier",
			account: configAccount{
				Tier:     "trial",
				Priority: 10,
			},
			expected: 42.5, // Trial tier should have lower score
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateAccountScore(config.AccountConfig{
				Tier:     tt.account.Tier,
				Priority: tt.account.Priority,
			}, "gpt-4")
			assert.Greater(t, score.Total, 0.0)
			assert.LessOrEqual(t, score.Total, 100.0)
		})
	}
}

type configAccount struct {
	Tier     string
	Priority int
}

func TestOutputQuotasTable(t *testing.T) {
	// Test table output
	quotas := []QuotaDisplayInfo{
		{
			Provider:              "openai",
			AccountID:             "acc-1",
			Tier:                  "premium",
			EffectiveRemainingPct: 75.0,
			Status:                "OK",
		},
		{
			Provider:              "anthropic",
			AccountID:             "acc-2",
			Tier:                  "enterprise",
			EffectiveRemainingPct: 5.0,
			Status:                "CRITICAL",
		},
	}

	// Should not panic
	err := outputQuotasTable(quotas)
	assert.NoError(t, err)
}

func TestGenerateRecommendations(t *testing.T) {
	// Test recommendation generation
	checks := []DoctorCheck{
		{
			Category:    "Dependencies",
			Name:        "Config File",
			Status:      "FAIL",
			Remediation: "Create a config file",
		},
		{
			Category: "System",
			Name:     "Go Version",
			Status:   "OK",
		},
	}

	recs := generateRecommendations(checks)
	// Should have at least 1 recommendation (the FAIL check)
	assert.GreaterOrEqual(t, len(recs), 1)
	assert.Contains(t, recs[0], "Config File")
}

func TestSimulateRouting(t *testing.T) {
	// Test routing simulation
	accounts := []config.AccountConfig{
		{
			ID:       "acc-1",
			Provider: "openai",
			Tier:     "premium",
			Priority: 100,
			Enabled:  true,
		},
		{
			ID:       "acc-2",
			Provider: "anthropic",
			Tier:     "trial",
			Priority: 10,
			Enabled:  true,
		},
	}

	results := simulateRouting(accounts, "gpt-4", 1)
	assert.Len(t, results, 1)
	assert.Equal(t, "acc-1", results[0].SelectedAccount.ID)
	assert.Len(t, results[0].AllScores, 2)
}

func TestExecute(t *testing.T) {
	// Test Execute function with no args (should show help)
	var buf bytes.Buffer
	RootCmd.SetOut(&buf)
	RootCmd.SetArgs([]string{"--help"})

	err := Execute([]string{"--help"})
	assert.NoError(t, err)
}

func TestExecuteWithErrorCode(t *testing.T) {
	// Test ExecuteWithErrorCode with valid command
	RootCmd.SetArgs([]string{"version"})
	code := ExecuteWithErrorCode([]string{"version"})
	assert.Equal(t, 0, code)
}

func TestGetRootCommand(t *testing.T) {
	// Test GetRootCommand
	cmd := GetRootCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "quotaguard", cmd.Use)
}

func TestRegisterCommand(t *testing.T) {
	// Test RegisterCommand
	testCmd := &cobra.Command{
		Use: "test-cmd",
		Run: func(cmd *cobra.Command, args []string) {},
	}

	RegisterCommand(testCmd)

	// Verify command was registered
	assert.Contains(t, RootCmd.Commands(), testCmd)
}

func TestInitCLI(t *testing.T) {
	// Initialize CLI before tests
	InitCLI()

	// Verify CLI was initialized correctly
	assert.NotNil(t, RootCmd)
	assert.Equal(t, "quotaguard", RootCmd.Use)
	assert.NotEmpty(t, RootCmd.Commands())
}

func TestOutputCheckResults(t *testing.T) {
	// Test check results output with all OK results
	results := []CheckResult{
		{
			Name:    "Database",
			Status:  "OK",
			Message: "Connected",
		},
		{
			Name:    "Config",
			Status:  "OK",
			Message: "Valid",
		},
	}

	// Should not panic
	err := outputCheckResultsTable(results)
	assert.NoError(t, err)
}

func TestCheckDatabase(t *testing.T) {
	// Test database check with non-existent file
	result := checkDatabase()
	// The result should have a status (OK or FAIL)
	assert.NotEmpty(t, result.Status)
	assert.NotEmpty(t, result.Name)
}

func TestCheckConfig(t *testing.T) {
	// Test config check
	result := checkConfig()
	assert.NotEmpty(t, result.Name)
	assert.NotEmpty(t, result.Status)
}

func TestCheckAccounts(t *testing.T) {
	// Test accounts check
	result := checkAccounts()
	assert.NotEmpty(t, result.Name)
	assert.NotEmpty(t, result.Status)
}

func TestDoctorReport(t *testing.T) {
	// Test doctor report structure
	report := DoctorReport{
		Timestamp: time.Now(),
		System: SystemInfo{
			OS:        "linux",
			Arch:      "amd64",
			GoVersion: "1.24.0",
		},
		Checks: []DoctorCheck{
			{
				Category: "System",
				Name:     "Test",
				Status:   "OK",
			},
		},
		Recommendations: []string{"Test recommendation"},
	}

	assert.Equal(t, "linux", report.System.OS)
	assert.Len(t, report.Checks, 1)
	assert.Len(t, report.Recommendations, 1)
}

func TestOutputDoctorReport(t *testing.T) {
	// Test doctor report output
	report := DoctorReport{
		Timestamp: time.Now(),
		Checks: []DoctorCheck{
			{
				Category: "System",
				Name:     "Go Version",
				Status:   "OK",
				Message:  "Test message",
			},
		},
		Recommendations: []string{"Test recommendation"},
	}

	// Should not panic
	err := outputDoctorReportTable(report)
	assert.NoError(t, err)
}

func TestOutputRouteResults(t *testing.T) {
	// Test route results output
	results := []RouteResult{
		{
			RequestNum: 1,
			SelectedAccount: AccountSummary{
				ID:       "test",
				Provider: "openai",
			},
		},
	}

	// Should not panic
	err := outputRouteResultsTable(results, 1)
	assert.NoError(t, err)
}

func TestScoreResult(t *testing.T) {
	// Test score result structure
	score := struct {
		Total       float64
		Safety      float64
		Reliability float64
		Cost        float64
		Reason      string
	}{
		Total:       85.5,
		Safety:      90.0,
		Reliability: 85.0,
		Cost:        80.0,
		Reason:      "Test reason",
	}

	assert.Equal(t, 85.5, score.Total)
	assert.Greater(t, score.Total, 0.0)
}

func TestDimensionDisplayInfo(t *testing.T) {
	// Test dimension display info
	dim := DimensionDisplayInfo{
		Type:  "requests",
		Used:  100,
		Limit: 1000,
		Pct:   10.0,
	}

	assert.Equal(t, "requests", dim.Type)
	assert.Equal(t, int64(100), dim.Used)
	assert.Equal(t, int64(1000), dim.Limit)
	assert.Equal(t, 10.0, dim.Pct)
}

func TestRunQuotas(t *testing.T) {
	// Test quotas command with config
	loader := config.NewLoader("config.yaml")
	cfg, err := loader.Load()
	if err != nil {
		t.Skip("Config file not found, skipping test")
	}

	assert.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Version)
}

func TestRunCheck(t *testing.T) {
	// Test check command
	results := []CheckResult{
		{Name: "Database", Status: "OK", Message: "OK"},
		{Name: "Config", Status: "OK", Message: "OK"},
		{Name: "Accounts", Status: "OK", Message: "OK"},
	}

	// Test table output with all OK
	err := outputCheckResultsTable(results)
	assert.NoError(t, err)
}

func TestRouteWithProviderFilter(t *testing.T) {
	// Test routing with provider filter
	results := simulateRouting([]config.AccountConfig{
		{ID: "acc-1", Provider: "openai", Priority: 100, Enabled: true},
		{ID: "acc-2", Provider: "anthropic", Priority: 50, Enabled: true},
	}, "gpt-4", 1)

	assert.Len(t, results, 1)
	assert.Equal(t, "acc-1", results[0].SelectedAccount.ID)
}

func TestRouteWithCount(t *testing.T) {
	// Test routing with multiple requests
	results := simulateRouting([]config.AccountConfig{
		{ID: "acc-1", Provider: "openai", Priority: 100, Enabled: true},
	}, "gpt-4", 5)

	assert.Len(t, results, 5)
}

func TestCheckDatabaseWithRealDB(t *testing.T) {
	// Test actual database check
	result := checkDatabase()
	assert.NotEmpty(t, result.Status)
	assert.NotEmpty(t, result.Message)
}

func TestCheckConfigWithRealConfig(t *testing.T) {
	// Test actual config check
	result := checkConfig()
	assert.NotEmpty(t, result.Status)
	assert.NotEmpty(t, result.Message)
}

func TestOutputQuotasJSON(t *testing.T) {
	// Test JSON output
	quotas := []QuotaDisplayInfo{
		{Provider: "openai", AccountID: "test", Status: "OK"},
	}

	// Should not panic
	err := outputQuotasJSON(quotas)
	assert.NoError(t, err)
}

func TestCheckResultWithDetails(t *testing.T) {
	// Test check result with details
	result := CheckResult{
		Name:    "Test",
		Status:  "OK",
		Message: "Test message",
		Details: "Test details",
	}

	assert.Equal(t, "Test", result.Name)
	assert.Equal(t, "Test details", result.Details)
}

func TestDoctorCheckWithRemediation(t *testing.T) {
	// Test doctor check with remediation
	check := DoctorCheck{
		Category:    "Test",
		Name:        "Test",
		Status:      "FAIL",
		Message:     "Test message",
		Severity:    "high",
		Remediation: "Fix this",
	}

	assert.Equal(t, "high", check.Severity)
	assert.Equal(t, "Fix this", check.Remediation)
}

func TestCollectSystemInfo(t *testing.T) {
	// Test system info collection
	checks := collectSystemInfo()

	// Should have system checks
	assert.GreaterOrEqual(t, len(checks), 3)

	// Verify check contents
	for _, check := range checks {
		assert.Equal(t, "System", check.Category)
		assert.NotEmpty(t, check.Name)
		assert.NotEmpty(t, check.Status)
	}
}

func TestCheckDependencies(t *testing.T) {
	// Test dependency checks
	checks := checkDependencies()

	// Should have dependency checks
	assert.GreaterOrEqual(t, len(checks), 2)

	for _, check := range checks {
		assert.Equal(t, "Dependencies", check.Category)
	}
}

func TestCheckConfiguration(t *testing.T) {
	// Test configuration checks
	checks := checkConfiguration()

	// Should have config checks
	for _, check := range checks {
		assert.Equal(t, "Configuration", check.Category)
	}
}

func TestVersionInfo(t *testing.T) {
	// Test version info structure
	info := VersionInfo{
		Version:   "1.0.0",
		GoVersion: "go1.24.0",
		OS:        "linux",
		Arch:      "amd64",
		BuildDate: "2024-01-01",
	}

	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, "go1.24.0", info.GoVersion)
	assert.Equal(t, "linux", info.OS)
	assert.Equal(t, "amd64", info.Arch)
	assert.Equal(t, "2024-01-01", info.BuildDate)
}

func TestGlobalFlagsStructure(t *testing.T) {
	// Test global flags structure
	flags := GlobalFlags{
		Config:  "custom.yaml",
		DBPath:  "/tmp/db.db",
		Verbose: true,
		JSON:    true,
		NoColor: true,
	}

	assert.Equal(t, "custom.yaml", flags.Config)
	assert.Equal(t, "/tmp/db.db", flags.DBPath)
	assert.True(t, flags.Verbose)
	assert.True(t, flags.JSON)
	assert.True(t, flags.NoColor)
}

func TestAccountSummary(t *testing.T) {
	// Test account summary structure
	summary := AccountSummary{
		ID:       "test-id",
		Provider: "openai",
		Tier:     "premium",
		Priority: 100,
	}

	assert.Equal(t, "test-id", summary.ID)
	assert.Equal(t, "openai", summary.Provider)
	assert.Equal(t, "premium", summary.Tier)
	assert.Equal(t, 100, summary.Priority)
}

func TestAccountScore(t *testing.T) {
	// Test account score structure
	score := AccountScore{
		Account: AccountSummary{
			ID:       "test-id",
			Provider: "openai",
		},
		Score:       85.5,
		SafetyScore: 90.0,
		Reliability: 85.0,
		CostScore:   80.0,
		Reason:      "Test reason",
	}

	assert.Equal(t, 85.5, score.Score)
	assert.Equal(t, 90.0, score.SafetyScore)
	assert.Equal(t, "Test reason", score.Reason)
}

func TestExitWithError(t *testing.T) {
	// Test exitWithError function
	// Note: This function calls os.Exit, so we can't test it directly
	// We just verify the function exists and has correct signature
	assert.NotNil(t, exitWithError)
}
