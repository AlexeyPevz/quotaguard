package cli

import (
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
)

var (
	cliInitialized bool
	cliInitMutex   sync.Mutex
)

// Execute runs the root command with the given arguments
func Execute(args []string) error {
	RootCmd.SetArgs(args)

	if err := RootCmd.Execute(); err != nil {
		return fmt.Errorf("command execution failed: %w", err)
	}

	return nil
}

// ExecuteWithErrorCode runs the root command and returns exit code
func ExecuteWithErrorCode(args []string) int {
	RootCmd.SetArgs(args)

	if err := RootCmd.Execute(); err != nil {
		if globalFlags.Verbose {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return 1
	}

	return 0
}

// GetRootCommand returns the root command
func GetRootCommand() *cobra.Command {
	return RootCmd
}

// RegisterCommand registers a new command with the root command
func RegisterCommand(cmd *cobra.Command) {
	RootCmd.AddCommand(cmd)
}

// InitCLI initializes the CLI framework with all commands
func InitCLI() {
	cliInitMutex.Lock()
	defer cliInitMutex.Unlock()

	if cliInitialized {
		return
	}

	// Initialize root command
	InitRoot()

	// Commands are auto-registered via their init() functions

	cliInitialized = true
}

// IsCLIInitialized returns true if CLI has been initialized
func IsCLIInitialized() bool {
	cliInitMutex.Lock()
	defer cliInitMutex.Unlock()
	return cliInitialized
}

// RunVersion runs the version command
func RunVersion() error {
	versionCmd.Run(versionCmd, []string{})
	return nil
}
