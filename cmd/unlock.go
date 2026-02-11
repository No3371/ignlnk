package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"github.com/user/ignlnk/internal/core"
)

func unlockCmd() *cli.Command {
	return &cli.Command{
		Name:      "unlock",
		Usage:     "Unlock files (replace placeholders with symlinks)",
		ArgsUsage: "<path>...",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) == 0 {
				return fmt.Errorf("no files specified")
			}

			project, err := core.FindProject(".")
			if err != nil {
				return err
			}
			vault, err := core.ResolveVault(project.Root)
			if err != nil {
				return err
			}

			unlock, err := project.LockManifest()
			if err != nil {
				return err
			}
			defer unlock()

			manifest, err := project.LoadManifest()
			if err != nil {
				return err
			}

			cleanup := installSignalHandler(project, manifest)
			defer cleanup()

			succeeded := 0
			failed := 0

			for _, arg := range args {
				relPath, err := project.RelPath(arg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: %s: %v\n", arg, err)
					failed++
					continue
				}

				if entry, ok := manifest.Files[relPath]; ok && entry.State == "unlocked" {
					fmt.Printf("already unlocked: %s\n", filepath.FromSlash(relPath))
					succeeded++
					continue
				}

				if err := core.UnlockFile(project, vault, manifest, relPath); err != nil {
					fmt.Fprintf(os.Stderr, "error: %s: %v\n", filepath.FromSlash(relPath), err)
					failed++
					continue
				}

				fmt.Printf("unlocked: %s\n", filepath.FromSlash(relPath))
				succeeded++
			}

			if err := project.SaveManifest(manifest); err != nil {
				return fmt.Errorf("saving manifest: %w", err)
			}

			if failed > 0 {
				return fmt.Errorf("%d of %d files unlocked, %d failed", succeeded, len(args), failed)
			}
			return nil
		},
	}
}
