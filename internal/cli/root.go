package cli

import (
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// GlobalFlags contains global flags available for all commands
type GlobalFlags struct {
	Config  string
	DBPath  string
	Verbose bool
	JSON    bool
	NoColor bool
}

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "quotaguard",
	Short: "QuotaGuard - AI Model Routing and Quota Management",
	Long: `QuotaGuard is a comprehensive solution for managing AI model quotas,
routing requests across multiple providers, and monitoring usage.

It provides intelligent routing, quota management, health monitoring,
and alerting capabilities for AI API providers.

Usage:
  quotaguard [command] [flags]

Available Commands:
  serve      Start the QuotaGuard server (main mode)
  setup      Discover CLIProxyAPI auths and sync accounts
  quotas     Show current quotas for all accounts
  check      Zero-config health check
  route      Test routing without execution
  doctor     Diagnose system and configuration issues

Flags:
  --config string   Path to configuration file (default "config.yaml")
  --db string       Path to SQLite database (default "./data/quotaguard.db")
  --verbose         Enable verbose output
  --json            Output in JSON format
  --no-color        Disable colored output

Use "quotaguard [command] --help" for more information about a command.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// InitRoot initializes the root command with global flags
func InitRoot() {
	configPath := os.Getenv("QUOTAGUARD_CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	dbPath := os.Getenv("QUOTAGUARD_DB_PATH")
	if dbPath == "" {
		dbPath = "./data/quotaguard.db"
	}

	RootCmd.PersistentFlags().StringVar(&globalFlags.Config, "config", configPath, "Path to configuration file")
	RootCmd.PersistentFlags().StringVar(&globalFlags.DBPath, "db", dbPath, "Path to SQLite database")
	RootCmd.PersistentFlags().BoolVarP(&globalFlags.Verbose, "verbose", "v", false, "Enable verbose output")
	RootCmd.PersistentFlags().BoolVar(&globalFlags.JSON, "json", false, "Output in JSON format")
	RootCmd.PersistentFlags().BoolVar(&globalFlags.NoColor, "no-color", false, "Disable colored output")

	// Add version command
	RootCmd.AddCommand(versionCmd)
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of QuotaGuard",
	Long:  `All software has versions. This is QuotaGuard's`,
	Run: func(cmd *cobra.Command, args []string) {
		printVersion()
	},
}

var globalFlags GlobalFlags

// GetGlobalFlags returns the global flags
func GetGlobalFlags() GlobalFlags {
	return globalFlags
}

// printVersion prints the version information
func printVersion() {
	info := GetVersionInfo()
	println("QuotaGuard Version:", info.Version)
	println("Go Version:", info.GoVersion)
	println("OS/Arch:", info.OS+"/"+info.Arch)
	println("Build Date:", info.BuildDate)
}

// VersionInfo contains version information
type VersionInfo struct {
	Version   string
	GoVersion string
	OS        string
	Arch      string
	BuildDate string
}

// GetVersionInfo returns version information
func GetVersionInfo() VersionInfo {
	return VersionInfo{
		Version:   "0.1.0",
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		BuildDate: "unknown",
	}
}

// exitWithError prints an error and exits with code 1
func exitWithError(err error) {
	if globalFlags.Verbose && err != nil {
		println("Error:", err.Error())
	}
	os.Exit(1)
}
