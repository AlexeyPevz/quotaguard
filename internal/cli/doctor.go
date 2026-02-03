package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/user"
	"runtime"
	"text/tabwriter"
	"time"

	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/spf13/cobra"
)

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose system and configuration issues",
	Long: `Perform a comprehensive system diagnostic for QuotaGuard.

This command checks:
- System information (OS, Go version, etc.)
- Dependencies availability
- Configuration issues
- Recommendations for fixes

Example:
  quotaguard doctor`,
	RunE: runDoctor,
}

func init() {
	RootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	if globalFlags.Verbose {
		log.Println("Starting system diagnostic...")
	}

	diagnostics := DoctorReport{
		Timestamp: time.Now().UTC(),
		Checks:    []DoctorCheck{},
	}

	// System info
	diagnostics.Checks = append(diagnostics.Checks, collectSystemInfo()...)

	// Dependency checks
	diagnostics.Checks = append(diagnostics.Checks, checkDependencies()...)

	// Configuration checks
	diagnostics.Checks = append(diagnostics.Checks, checkConfiguration()...)

	// Recommendations
	diagnostics.Recommendations = generateRecommendations(diagnostics.Checks)

	// Output results
	return outputDoctorReport(diagnostics)
}

// DoctorReport represents the complete diagnostic report
type DoctorReport struct {
	Timestamp       time.Time     `json:"timestamp"`
	System          SystemInfo    `json:"system"`
	Checks          []DoctorCheck `json:"checks"`
	Recommendations []string      `json:"recommendations"`
}

// SystemInfo contains system information
type SystemInfo struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	GoVersion   string `json:"go_version"`
	CPUs        int    `json:"cpus"`
	MemoryTotal uint64 `json:"memory_total_gb"`
	User        string `json:"user"`
	WorkingDir  string `json:"working_dir"`
}

// DoctorCheck represents a single diagnostic check
type DoctorCheck struct {
	Category    string `json:"category"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Severity    string `json:"severity,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

func collectSystemInfo() []DoctorCheck {
	checks := []DoctorCheck{}

	// Get system info
	sysInfo := SystemInfo{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		CPUs:      runtime.NumCPU(),
	}

	// Get user info
	if u, err := user.Current(); err == nil {
		sysInfo.User = u.Username
	} else {
		sysInfo.User = "unknown"
	}

	// Get working directory
	if wd, err := os.Getwd(); err == nil {
		sysInfo.WorkingDir = wd
	} else {
		sysInfo.WorkingDir = "unknown"
	}

	checks = append(checks, DoctorCheck{
		Category: "System",
		Name:     "Operating System",
		Status:   "OK",
		Message:  fmt.Sprintf("OS: %s (%s)", sysInfo.OS, sysInfo.Arch),
	})

	checks = append(checks, DoctorCheck{
		Category: "System",
		Name:     "Go Version",
		Status:   "OK",
		Message:  fmt.Sprintf("Go: %s (CPUs: %d)", sysInfo.GoVersion, sysInfo.CPUs),
	})

	checks = append(checks, DoctorCheck{
		Category: "System",
		Name:     "User",
		Status:   "OK",
		Message:  fmt.Sprintf("User: %s", sysInfo.User),
	})

	checks = append(checks, DoctorCheck{
		Category: "System",
		Name:     "Working Directory",
		Status:   "OK",
		Message:  fmt.Sprintf("Directory: %s", sysInfo.WorkingDir),
	})

	return checks
}

func checkDependencies() []DoctorCheck {
	checks := []DoctorCheck{}

	// Check config file
	checks = append(checks, checkConfigFile())

	// Check database file
	checks = append(checks, checkDatabaseFile())

	return checks
}

func checkConfigFile() DoctorCheck {
	check := DoctorCheck{
		Category: "Dependencies",
		Name:     "Config File",
	}

	loader := config.NewLoader(globalFlags.Config)
	_, err := loader.Load()
	if err != nil {
		check.Status = "FAIL"
		check.Message = fmt.Sprintf("Config file not found or invalid: %v", err)
		check.Severity = "high"
		check.Remediation = "Create a valid config.yaml file or specify --config flag"
		return check
	}

	check.Status = "OK"
	check.Message = fmt.Sprintf("Config file found: %s", globalFlags.Config)
	return check
}

func checkDatabaseFile() DoctorCheck {
	check := DoctorCheck{
		Category: "Dependencies",
		Name:     "Database File",
	}

	// Check if database directory exists
	dbDir := globalFlags.DBPath
	if dbDir == "./data/quotaguard.db" {
		dbDir = "./data"
	}

	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		check.Status = "WARN"
		check.Message = fmt.Sprintf("Database directory does not exist: %s", dbDir)
		check.Severity = "medium"
		check.Remediation = "The database will be created automatically when starting the server"
		return check
	}

	check.Status = "OK"
	check.Message = fmt.Sprintf("Database path: %s", globalFlags.DBPath)
	return check
}

func checkConfiguration() []DoctorCheck {
	checks := []DoctorCheck{}

	loader := config.NewLoader(globalFlags.Config)
	cfg, err := loader.Load()
	if err != nil {
		checks = append(checks, DoctorCheck{
			Category:    "Configuration",
			Name:        "Config Load",
			Status:      "FAIL",
			Message:     fmt.Sprintf("Failed to load config: %v", err),
			Severity:    "high",
			Remediation: "Check config.yaml syntax and file permissions",
		})
		return checks
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		checks = append(checks, DoctorCheck{
			Category:    "Configuration",
			Name:        "Config Validation",
			Status:      "FAIL",
			Message:     fmt.Sprintf("Config validation failed: %v", err),
			Severity:    "high",
			Remediation: "Review config.yaml for invalid values",
		})
	}

	// Check server config
	if cfg.Server.HTTPPort == 0 {
		checks = append(checks, DoctorCheck{
			Category:    "Configuration",
			Name:        "Server Port",
			Status:      "WARN",
			Message:     "Server port not configured, using default",
			Severity:    "low",
			Remediation: "Set server.http_port in config.yaml",
		})
	}

	// Check accounts
	if len(cfg.Accounts) == 0 {
		checks = append(checks, DoctorCheck{
			Category:    "Configuration",
			Name:        "Accounts",
			Status:      "WARN",
			Message:     "No accounts configured",
			Severity:    "medium",
			Remediation: "Add accounts to config.yaml for quota routing",
		})
	} else {
		checks = append(checks, DoctorCheck{
			Category: "Configuration",
			Name:     "Accounts",
			Status:   "OK",
			Message:  fmt.Sprintf("%d accounts configured", len(cfg.Accounts)),
		})
	}

	return checks
}

func generateRecommendations(checks []DoctorCheck) []string {
	recommendations := []string{}

	failCount := 0
	warnCount := 0

	for _, check := range checks {
		if check.Status == "FAIL" {
			failCount++
			if check.Remediation != "" {
				recommendations = append(recommendations, fmt.Sprintf("[%s] %s: %s", check.Category, check.Name, check.Remediation))
			}
		}
		if check.Status == "WARN" {
			warnCount++
		}
	}

	if failCount == 0 && warnCount == 0 {
		recommendations = append(recommendations, "System is healthy. No recommendations needed.")
	} else if failCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d critical issue(s) and %d warning(s). Please address the critical issues first.", failCount, warnCount))
	}

	return recommendations
}

func outputDoctorReport(report DoctorReport) error {
	if globalFlags.JSON {
		return outputDoctorReportJSON(report)
	}
	return outputDoctorReportTable(report)
}

func outputDoctorReportJSON(report DoctorReport) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func outputDoctorReportTable(report DoctorReport) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	fmt.Println("=== QuotaGuard Doctor Report ===")
	fmt.Printf("Generated: %s\n\n", report.Timestamp.Format(time.RFC3339))

	fmt.Println("--- System Information ---")
	for _, check := range report.Checks {
		if check.Category == "System" {
			statusIcon := "✓"
			if check.Status != "OK" {
				statusIcon = "!"
			}
			fmt.Fprintf(w, "%s %s: %s\n", statusIcon, check.Name, check.Message)
		}
	}

	fmt.Println("\n--- Dependencies ---")
	for _, check := range report.Checks {
		if check.Category == "Dependencies" {
			statusIcon := "✓"
			if check.Status == "FAIL" {
				statusIcon = "✗"
			} else if check.Status == "WARN" {
				statusIcon = "!"
			}
			fmt.Fprintf(w, "%s %s: %s\n", statusIcon, check.Name, check.Message)
		}
	}

	fmt.Println("\n--- Configuration ---")
	for _, check := range report.Checks {
		if check.Category == "Configuration" {
			statusIcon := "✓"
			if check.Status == "FAIL" {
				statusIcon = "✗"
			} else if check.Status == "WARN" {
				statusIcon = "!"
			}
			fmt.Fprintf(w, "%s %s: %s\n", statusIcon, check.Name, check.Message)
		}
	}

	if err := w.Flush(); err != nil {
		log.Printf("Error flushing tabwriter: %v", err)
	}

	fmt.Println("\n--- Recommendations ---")
	if len(report.Recommendations) > 0 {
		for _, rec := range report.Recommendations {
			fmt.Printf("• %s\n", rec)
		}
	} else {
		fmt.Println("No recommendations.")
	}

	return nil
}
