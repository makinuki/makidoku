package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/backup"
	"github.com/makinuki/makidoku/internal/db"
)

var exportCmd = &cobra.Command{
	Use:   "export [--output file.json]",
	Short: "Export library backup to JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer database.Close()

		data, err := backup.Export(database)
		if err != nil {
			return err
		}
		if output == "" || output == "-" {
			_, err = os.Stdout.Write(data)
			return err
		}
		if err := os.WriteFile(output, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "exported to %s\n", output)
		return nil
	},
}

var importCmd = &cobra.Command{
	Use:   "import <file.json>",
	Short: "Import library backup from JSON",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer database.Close()
		return backup.Import(database, data)
	},
}

func init() {
	exportCmd.Flags().StringP("output", "o", "-", "output file (\"-\" for stdout)")
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
}
