package cli

import (
	"fmt"
	"time"

	"github.com/quotaguard/quotaguard/internal/cliproxy"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/spf13/cobra"
)

// setupCmd represents the setup/import command
var setupCmd = &cobra.Command{
	Use:     "setup [auths_path]",
	Aliases: []string{"import", "sync"},
	Short:   "Discover CLIProxyAPI auths and sync accounts to SQLite",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runSetup,
}

func init() {
	RootCmd.AddCommand(setupCmd)
}

func runSetup(cmd *cobra.Command, args []string) error {
	preferredPath := ""
	if len(args) > 0 {
		preferredPath = args[0]
	}

	authPath := cliproxy.ResolveAuthPath(preferredPath)
	if authPath == "" {
		return fmt.Errorf("no auths path resolved; set QUOTAGUARD_CLIPROXY_AUTH_PATH or pass a path")
	}

	if !cliproxy.HasAuthFiles(authPath) {
		return fmt.Errorf("no CLIProxyAPI auth files found in %s", authPath)
	}

	sqliteStore, err := store.NewSQLiteStore(globalFlags.DBPath)
	if err != nil {
		return fmt.Errorf("failed to create SQLite store: %w", err)
	}
	defer sqliteStore.Close()

	manager := cliproxy.NewAccountManager(sqliteStore, authPath, 5*time.Minute)
	newCount, updatedCount, err := manager.ScanAndSync()
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	fmt.Printf("Sync complete: %d new, %d updated\n", newCount, updatedCount)
	return nil
}
