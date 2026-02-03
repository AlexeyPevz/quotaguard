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

// routeCmd represents the route command
var routeCmd = &cobra.Command{
	Use:     "route",
	Aliases: []string{"r", "routing", "router"},
	Short:   "Test routing without execution",
	Long: `Test routing decisions without actually making requests.

This command simulates routing based on the current quota state
and shows which account would be selected for a given request.

Examples:
  # Dry-run routing for a GPT-4 request
  quotaguard route --model gpt-4 --prompt "Hello" --dry-run

  # Simulate load with multiple requests
  quotaguard route --model gpt-4 --prompt "Hello" --count 10 --dry-run

  # Show routing scores for each account
  quotaguard route --model gpt-4 --verbose`,
	RunE: runRoute,
}

var routeFlags struct {
	Model    string
	Prompt   string
	DryRun   bool
	Count    int
	Provider string
}

func init() {
	routeCmd.Flags().StringVar(&routeFlags.Model, "model", "gpt-4", "Model to route for")
	routeCmd.Flags().StringVar(&routeFlags.Prompt, "prompt", "", "Prompt for the request")
	routeCmd.Flags().BoolVar(&routeFlags.DryRun, "dry-run", true, "Simulate routing without execution")
	routeCmd.Flags().IntVar(&routeFlags.Count, "count", 1, "Number of requests to simulate")
	routeCmd.Flags().StringVar(&routeFlags.Provider, "provider", "", "Filter by provider")

	RootCmd.AddCommand(routeCmd)
}

func runRoute(cmd *cobra.Command, args []string) error {
	if globalFlags.Verbose {
		log.Printf("Starting routing simulation for model: %s", routeFlags.Model)
		log.Printf("Dry-run mode: %v", routeFlags.DryRun)
	}

	// Load configuration
	loader := config.NewLoader(globalFlags.Config)
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Filter accounts
	var accounts []config.AccountConfig
	for _, acc := range cfg.Accounts {
		if routeFlags.Provider != "" && acc.Provider != routeFlags.Provider {
			continue
		}
		if !acc.Enabled {
			continue
		}
		accounts = append(accounts, acc)
	}

	if len(accounts) == 0 {
		return fmt.Errorf("no enabled accounts found")
	}

	// Simulate routing
	results := simulateRouting(accounts, routeFlags.Model, routeFlags.Count)

	// Output results
	if globalFlags.JSON {
		return outputRouteResultsJSON(results)
	}
	return outputRouteResultsTable(results, routeFlags.Count)
}

// RouteResult represents the result of a routing simulation
type RouteResult struct {
	RequestNum      int            `json:"request_num"`
	SelectedAccount AccountSummary `json:"selected_account"`
	AllScores       []AccountScore `json:"all_scores"`
}

// AccountSummary represents a selected account
type AccountSummary struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Tier     string `json:"tier"`
	Priority int    `json:"priority"`
}

// AccountScore represents routing score for an account
type AccountScore struct {
	Account     AccountSummary `json:"account"`
	Score       float64        `json:"score"`
	SafetyScore float64        `json:"safety_score"`
	Reliability float64        `json:"reliability"`
	CostScore   float64        `json:"cost_score"`
	Reason      string         `json:"reason"`
}

func simulateRouting(accounts []config.AccountConfig, model string, count int) []RouteResult {
	results := []RouteResult{}

	for i := 0; i < count; i++ {
		result := RouteResult{
			RequestNum: i + 1,
			AllScores:  []AccountScore{},
		}

		// Calculate scores for each account
		var bestScore float64 = -1
		var bestAccount config.AccountConfig

		for _, acc := range accounts {
			score := calculateAccountScore(acc, model)
			accountSummary := AccountSummary{
				ID:       acc.ID,
				Provider: acc.Provider,
				Tier:     acc.Tier,
				Priority: acc.Priority,
			}
			accountScore := AccountScore{
				Account:     accountSummary,
				Score:       score.Total,
				SafetyScore: score.Safety,
				Reliability: score.Reliability,
				CostScore:   score.Cost,
				Reason:      score.Reason,
			}
			result.AllScores = append(result.AllScores, accountScore)

			if score.Total > bestScore {
				bestScore = score.Total
				bestAccount = acc
			}
		}

		result.SelectedAccount = AccountSummary{
			ID:       bestAccount.ID,
			Provider: bestAccount.Provider,
			Tier:     bestAccount.Tier,
			Priority: bestAccount.Priority,
		}

		results = append(results, result)
	}

	return results
}

// ScoreResult represents the calculated score for an account
type ScoreResult struct {
	Total       float64
	Safety      float64
	Reliability float64
	Cost        float64
	Reason      string
}

func calculateAccountScore(acc config.AccountConfig, model string) ScoreResult {
	// Simple scoring algorithm based on tier and priority
	// Higher priority accounts get higher scores
	// Higher tiers (e.g., "premium") get better reliability scores

	safety := 100.0
	reliability := 90.0
	cost := 80.0
	reason := "Default routing"

	// Adjust based on tier
	switch acc.Tier {
	case "premium":
		reliability = 95.0
		cost = 70.0
		reason = "Premium tier - higher reliability"
	case "enterprise":
		reliability = 98.0
		cost = 60.0
		reason = "Enterprise tier - highest reliability"
	case "trial":
		reliability = 70.0
		safety = 80.0
		reason = "Trial tier - limited reliability"
	}

	// Adjust based on priority (0-100)
	priorityFactor := float64(acc.Priority) / 100.0
	safety = safety * (0.5 + 0.5*priorityFactor)
	reliability = reliability * (0.7 + 0.3*priorityFactor)

	// Calculate total score (weighted average)
	total := safety*0.4 + reliability*0.4 + cost*0.2

	return ScoreResult{
		Total:       total,
		Safety:      safety,
		Reliability: reliability,
		Cost:        cost,
		Reason:      reason,
	}
}

func outputRouteResultsJSON(results []RouteResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func outputRouteResultsTable(results []RouteResult, count int) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	fmt.Printf("=== Routing Simulation for model: %s ===\n", routeFlags.Model)
	fmt.Printf("Requests: %d, Dry-run: true\n\n", count)

	for _, result := range results {
		fmt.Fprintf(w, "Request #%d:\n", result.RequestNum)
		fmt.Fprintf(w, "  Selected: %s/%s (Tier: %s, Priority: %d)\n",
			result.SelectedAccount.Provider,
			result.SelectedAccount.ID,
			result.SelectedAccount.Tier,
			result.SelectedAccount.Priority,
		)

		if globalFlags.Verbose {
			fmt.Fprintln(w, "  Scores:")
			for _, score := range result.AllScores {
				fmt.Fprintf(w, "    %s/%s: %.2f (Safety: %.1f, Reliability: %.1f, Cost: %.1f) - %s\n",
					score.Account.Provider,
					score.Account.ID,
					score.Score,
					score.SafetyScore,
					score.Reliability,
					score.CostScore,
					score.Reason,
				)
			}
		}
	}

	if err := w.Flush(); err != nil {
		log.Printf("Error flushing tabwriter: %v", err)
	}

	return nil
}
