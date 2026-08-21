package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
	"github.com/makinuki/makidoku/internal/engine"
)

type downloadRunner interface {
	EnqueueManga(context.Context, string, downloader.ChapterSelection, string) ([]db.DownloadQueueItem, error)
	Drain(context.Context) error
}

var downloadCmd = &cobra.Command{
	Use:   "download <source:manga-id> [--chapters 1-50] [--format cbz|folder]",
	Short: "Headless batch download",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		chapterRange, _ := cmd.Flags().GetString("chapters")
		format, _ := cmd.Flags().GetString("format")
		if format != downloader.FormatCBZ && format != downloader.FormatFolder {
			return fmt.Errorf("format must be cbz or folder")
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

		queue := downloader.NewQueue(db.NewRepository(database), eng, downloader.Options{
			Workers: cfg.DownloadWorkers, PageInterval: cfg.PageInterval,
			DownloadDir: cfg.DownloadDir, MaxRetries: 3,
		})
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		count, err := executeDownload(ctx, queue, args[0], chapterRange, format)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "downloaded %d chapter(s)\n", count)
		return nil
	},
}

func executeDownload(ctx context.Context, runner downloadRunner, mangaID, chapterRange, format string) (int, error) {
	items, err := runner.EnqueueManga(ctx, mangaID, downloader.ChapterSelection{Range: chapterRange}, format)
	if err != nil {
		return 0, err
	}
	if err := runner.Drain(ctx); err != nil {
		return len(items), err
	}
	return len(items), nil
}

func init() {
	downloadCmd.Flags().String("chapters", "", "chapter range, e.g. 1-50 (default: all chapters)")
	downloadCmd.Flags().String("format", downloader.FormatCBZ, "archive format: cbz or folder")
	rootCmd.AddCommand(downloadCmd)
}
