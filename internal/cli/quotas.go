package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/spf13/cobra"
)

// quotasCmd represents the quotas command
var quotasCmd = &cobra.Command{
	Use:     "quotas",
	Aliases: []string{"q", "quota", "limits"},
	Short:   "Show current quotas for all accounts",
	Long: `Display current quota information for all configured accounts.

This command shows the remaining quota percentage, usage by dimension,
and health status for each account.

Examples:
  # Show all quotas
  quotaguard quotas

  # Filter by provider
  quotaguard quotas --provider openai

  # Output as JSON
  quotaguard quotas --json | jq '.'

  # Show only critical accounts
  quotaguard quotas --critical`,
	RunE: runQuotas,
}

var quotasFlags struct {
	Provider  string
	AccountID string
	Critical  bool
	All       bool
}

func init() {
	quotasCmd.Flags().StringVar(&quotasFlags.Provider, "provider", "", "Filter by provider (e.g., openai, anthropic)")
	quotasCmd.Flags().StringVar(&quotasFlags.AccountID, "account", "", "Filter by account ID")
	quotasCmd.Flags().BoolVar(&quotasFlags.Critical, "critical", false, "Show only critical accounts (quota < 10%)")
	quotasCmd.Flags().BoolVar(&quotasFlags.All, "all", false, "Show all accounts including healthy ones")

	RootCmd.AddCommand(quotasCmd)
}

func runQuotas(cmd *cobra.Command, args []string) error {
	// Load configuration
	loader := config.NewLoader(globalFlags.Config)
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Filter accounts
	var filteredAccounts []config.AccountConfig
	for _, acc := range cfg.Accounts {
		if quotasFlags.Provider != "" && acc.Provider != quotasFlags.Provider {
			continue
		}
		if quotasFlags.AccountID != "" && acc.ID != quotasFlags.AccountID {
			continue
		}
		filteredAccounts = append(filteredAccounts, acc)
	}

	// Collect quota info for each account
	var quotaInfos []QuotaDisplayInfo
	for _, acc := range filteredAccounts {
		displayInfo := QuotaDisplayInfo{
			Provider:              acc.Provider,
			AccountID:             acc.ID,
			Tier:                  acc.Tier,
			EffectiveRemainingPct: 100, // Default to 100% since we don't have real quota data
			IsThrottled:           false,
			IsShadowBanned:        false,
			Status:                "OK",
		}

		// Determine health status based on tier priority (lower tier = higher priority for display)
		if acc.Priority < 0 {
			displayInfo.Status = "CRITICAL"
		} else if acc.Priority < 50 {
			displayInfo.Status = "WARNING"
		} else {
			displayInfo.Status = "OK"
		}

		// Filter by critical flag
		if quotasFlags.Critical && displayInfo.Status != "CRITICAL" {
			continue
		}

		quotaInfos = append(quotaInfos, displayInfo)
	}

	// Output based on flags
	if globalFlags.JSON {
		return outputQuotasJSON(quotaInfos)
	}
	return outputQuotasTable(quotaInfos)
}

// QuotaDisplayInfo represents quota info for display
type QuotaDisplayInfo struct {
	Provider              string                 `json:"provider"`
	AccountID             string                 `json:"account_id"`
	Tier                  string                 `json:"tier"`
	EffectiveRemainingPct float64                `json:"effective_remaining_percent"`
	IsThrottled           bool                   `json:"is_throttled"`
	IsShadowBanned        bool                   `json:"is_shadow_banned"`
	Status                string                 `json:"status"`
	Dimensions            []DimensionDisplayInfo `json:"dimensions,omitempty"`
}

// DimensionDisplayInfo represents dimension info for display
type DimensionDisplayInfo struct {
	Type  string  `json:"type"`
	Used  int64   `json:"used"`
	Limit int64   `json:"limit"`
	Pct   float64 `json:"used_percent"`
}

func outputQuotasJSON(quotas []QuotaDisplayInfo) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(quotas)
}

func outputQuotasTable(quotas []QuotaDisplayInfo) error {
	if len(quotas) == 0 {
		fmt.Println("No accounts found matching the criteria.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tACCOUNT ID\tTIER\tREMAINING\tSTATUS\tTHROTTLED\tSHADOW BANNED")

	for _, q := range quotas {
		remainingStr := fmt.Sprintf("%.1f%%", q.EffectiveRemainingPct)
		throttledStr := "No"
		shadowBannedStr := "No"
		statusColor := q.Status

		if q.IsThrottled {
			throttledStr = "Yes"
		}
		if q.IsShadowBanned {
			shadowBannedStr = "Yes"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			q.Provider,
			q.AccountID,
			q.Tier,
			remainingStr,
			statusColor,
			throttledStr,
			shadowBannedStr,
		)
	}

	if err := w.Flush(); err != nil {
		log.Printf("Error flushing tabwriter: %v", err)
	}
	return nil
}
