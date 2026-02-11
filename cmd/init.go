package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"github.com/user/ignlnk/internal/core"
)

func initCmd() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Initialize ignlnk in the current directory",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			// Check if already initialized
			ignlnkDir := filepath.Join(cwd, ".ignlnk")
			if info, err := os.Stat(ignlnkDir); err == nil && info.IsDir() {
				fmt.Fprintln(os.Stderr, "warning: already initialized")
				return nil
			}

			// Initialize project
			project, err := core.InitProject(cwd)
			if err != nil {
				return err
			}

			// Symlink capability check (warn only, don't fail)
			if err := core.CheckSymlinkSupport(project.IgnlnkDir); err != nil {
				fmt.Fprintf(os.Stderr, "warning: symlinks not supported on this system. ignlnk unlock will not work until Developer Mode is enabled (Windows: Settings > Update & Security > For Developers).\n")
			}

			// Register in central index
			vault, err := core.RegisterProject(cwd)
			if err != nil {
				return err
			}

			fmt.Printf("Initialized ignlnk in %s\n", filepath.FromSlash(cwd))
			fmt.Printf("Vault: %s\n", filepath.FromSlash(vault.Dir))
			return nil
		},
	}
}
