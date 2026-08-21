package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

type installRunner interface {
	Install(context.Context, string) (engine.InstalledSource, error)
	InstallFile(context.Context, string) (engine.InstalledSource, error)
}

var installCmd = &cobra.Command{
	Use:   "install <id | --path <plugin.wasm>>",
	Short: "Install a source from the catalog or a local binary",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		catalogID := ""
		if len(args) == 1 {
			catalogID = args[0]
		}
		path, _ := cmd.Flags().GetString("path")
		id, path, err := parseInstallArgs(catalogID, path)
		if err != nil {
			return err
		}

		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer database.Close()
		eng := engine.New(database, engine.Options{
			DataDir: cfg.DataDir, RegistryURL: cfg.RegistryURL,
			ChallengeWait: cfg.ChallengeWait,
		})
		defer func() {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()
			eng.Close(closeCtx)
		}()

		source, err := executeInstall(cmd.Context(), eng, id, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed %s v%s (%s, lang %s, ABI %d)\n",
			source.Name, source.Version, strings.TrimRight(source.BaseURL, "/"),
			source.Lang, source.ABIVersion)
		return nil
	},
}

func executeInstall(ctx context.Context, runner installRunner, catalogID, path string) (engine.InstalledSource, error) {
	if path != "" {
		return runner.InstallFile(ctx, path)
	}
	return runner.Install(ctx, catalogID)
}

// parseInstallArgs normalizes and validates install arguments, returning the
// catalog id and plugin path to use. Exactly one of the two must be present.
func parseInstallArgs(catalogID, path string) (string, string, error) {
	catalogID = strings.TrimSpace(catalogID)
	path = strings.TrimSpace(path)
	switch {
	case catalogID == "" && path == "":
		return "", "", fmt.Errorf("provide a catalog id or --path to a plugin binary")
	case catalogID != "" && path != "":
		return "", "", fmt.Errorf("provide either a catalog id or --path, not both")
	}
	return catalogID, path, nil
}

func init() {
	installCmd.Flags().String("path", "", "install a locally built plugin binary instead of a catalog id")
	rootCmd.AddCommand(installCmd)
}