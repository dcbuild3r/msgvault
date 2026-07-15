package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	msgmatrix "go.kenn.io/msgvault/internal/matrix"
	"go.kenn.io/msgvault/internal/store"
)

var (
	syncMatrixFull          bool
	syncMatrixBackfillPages int
	syncMatrixNoMedia       bool
)

func newSyncMatrixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync-matrix",
		Short: "Synchronize Matrix rooms into the archive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isDaemonCLISubprocess() {
				return runDaemonCLICommandHTTPFromCobra(cmd, args)
			}
			imp, dbPath, cleanup, err := openMatrixImporter()
			if err != nil {
				return err
			}
			defer cleanup()
			ctx, stop := withInterruptCancel(cmd, "\nInterrupted. Matrix cursor has been checkpointed.")
			defer stop()
			opts := matrixImportOptions()
			opts.Full = syncMatrixFull
			opts.BackfillLimit = syncMatrixBackfillPages
			opts.NoMedia = opts.NoMedia || syncMatrixNoMedia
			sum, err := imp.Sync(ctx, opts)
			rebuildCacheAfterWrite(dbPath)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Matrix sync complete: %d rooms, %d events, %d attachments\n", sum.RoomsProcessed, sum.MessagesProcessed, sum.AttachmentsDownloaded)
			return nil
		},
	}
	cmd.Flags().BoolVar(&syncMatrixFull, "full", false, "reset stored cursors and repeat the available history backfill")
	cmd.Flags().IntVar(&syncMatrixBackfillPages, "backfill-pages", 0, "maximum history pages per room this run (0 = all)")
	cmd.Flags().BoolVar(&syncMatrixNoMedia, "no-media", false, "skip Matrix media downloads for this run")
	return cmd
}

func matrixImportOptions() msgmatrix.ImportOptions {
	return msgmatrix.ImportOptions{
		LongPollTimeout: 50 * time.Second,
		AttachmentsDir:  cfg.AttachmentsDir(),
		NoMedia:         !cfg.Matrix.MediaEnabled(),
		MaxMediaBytes:   cfg.Matrix.MaxMediaBytes(),
	}
}

func openMatrixImporter() (*msgmatrix.Importer, string, func(), error) {
	s, cleanup, err := openWritableStoreAndInitForIngest()
	if err != nil {
		return nil, "", nil, err
	}
	credentials, err := msgmatrix.LoadCredentials(cfg.TokensDir())
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	client, err := msgmatrix.NewClient(credentials.Homeserver, credentials.AccessToken)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return msgmatrix.NewImporter(s, client, credentials.UserID), cfg.DatabaseDSN(), cleanup, nil
}

func runConfiguredMatrixSync(ctx context.Context, s *store.Store) error {
	credentials, err := msgmatrix.LoadCredentials(cfg.TokensDir())
	if err != nil {
		return err
	}
	client, err := msgmatrix.NewClient(credentials.Homeserver, credentials.AccessToken)
	if err != nil {
		return err
	}
	_, err = msgmatrix.NewImporter(s, client, credentials.UserID).Sync(ctx, matrixImportOptions())
	if err == nil {
		rebuildCacheAfterScheduledSync(context.WithoutCancel(ctx), "matrix")
	}
	return err
}

func init() { rootCmd.AddCommand(newSyncMatrixCmd()) }
