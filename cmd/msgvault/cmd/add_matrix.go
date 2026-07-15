package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"go.kenn.io/msgvault/internal/clirun"
	msgmatrix "go.kenn.io/msgvault/internal/matrix"
)

var (
	addMatrixHomeserver string
	addMatrixUserID     string
	addMatrixTokenFile  string
)

func newAddMatrixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-matrix",
		Short: "Add a Matrix account as an archive source",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			homeserver := strings.TrimSpace(addMatrixHomeserver)
			if homeserver == "" {
				homeserver = cfg.Matrix.Homeserver
			}
			userID := strings.TrimSpace(addMatrixUserID)
			if userID == "" {
				userID = cfg.Matrix.UserID
			}
			if homeserver == "" || userID == "" {
				return errors.New("Matrix homeserver and user ID are required (flags or [matrix] config)")
			}
			token, err := readAddMatrixToken(cmd)
			if err != nil {
				return err
			}
			if !isDaemonCLISubprocess() {
				if !IsRemoteMode() {
					client, err := msgmatrix.NewClient(homeserver, token)
					if err != nil {
						return err
					}
					who, err := client.WhoAmI(cmd.Context())
					if err != nil {
						return err
					}
					if who.UserID != userID {
						return fmt.Errorf("Matrix token belongs to %s, not %s", who.UserID, userID)
					}
				}
				return runDaemonCLICommandHTTPFromCobraWithEnv(cmd, args, map[string]string{clirun.EnvMatrixToken: token})
			}

			client, err := msgmatrix.NewClient(homeserver, token)
			if err != nil {
				return err
			}
			who, err := client.WhoAmI(cmd.Context())
			if err != nil {
				return err
			}
			if who.UserID != userID {
				return fmt.Errorf("Matrix token belongs to %s, not %s", who.UserID, userID)
			}
			if err := msgmatrix.SaveCredentials(cfg.TokensDir(), msgmatrix.Credentials{Homeserver: homeserver, UserID: userID, AccessToken: token}); err != nil {
				return err
			}
			s, cleanup, err := openWritableStoreAndInitForIngest()
			if err != nil {
				return err
			}
			defer cleanup()
			source, err := s.GetOrCreateSource(sourceTypeMatrix, userID)
			if err != nil {
				return err
			}
			if err := s.UpdateSourceDisplayName(source.ID, "Matrix "+userID); err != nil {
				return err
			}
			if err := runPostSourceCreateMigrations(s); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Matrix archive source added: %s\n", userID)
			return nil
		},
	}
	cmd.Flags().StringVar(&addMatrixHomeserver, "homeserver", "", "Matrix homeserver client API URL")
	cmd.Flags().StringVar(&addMatrixUserID, "user-id", "", "Matrix archive identity (for example @archive:example.org)")
	cmd.Flags().StringVar(&addMatrixTokenFile, "token-file", "", "read the Matrix access token from this file")
	return cmd
}

func readAddMatrixToken(cmd *cobra.Command) (string, error) {
	if token := strings.TrimSpace(os.Getenv(clirun.EnvMatrixToken)); token != "" {
		return token, nil
	}
	if addMatrixTokenFile != "" {
		data, err := os.ReadFile(addMatrixTokenFile)
		if err != nil {
			return "", fmt.Errorf("read Matrix token file: %w", err)
		}
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
		return "", errors.New("Matrix token file is empty")
	}
	method, output := choosePasswordStrategy(
		isatty.IsTerminal(os.Stdin.Fd()), isatty.IsCygwinTerminal(os.Stdin.Fd()),
		isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd()),
		isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()),
	)
	switch method {
	case passwordInteractive:
		return readPasswordInteractive("Matrix access token:", output)
	case passwordPipe:
		return readPasswordFromPipe(os.Stdin)
	default:
		return "", errors.New("cannot read Matrix token: use --token-file, stdin, or MSGVAULT_MATRIX_TOKEN")
	}
}

func init() { rootCmd.AddCommand(newAddMatrixCmd()) }
