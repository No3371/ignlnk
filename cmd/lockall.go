package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"github.com/user/ignlnk/internal/core"
	"github.com/user/ignlnk/internal/ignlnkfiles"
)

func lockAllCmd() *cli.Command {
	return &cli.Command{
		Name:  "lock-all",
		Usage: "Lock all managed and .ignlnkfiles-matched files",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Preview files that would be locked without locking them",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Allow locking files larger than 1GB",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
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

			// Discover new files from .ignlnkfiles
			var newFiles []string
			ignlnkfilesPath := filepath.Join(project.Root, ".ignlnkfiles")
			if _, err := os.Stat(ignlnkfilesPath); err == nil {
				ignorer, err := ignlnkfiles.Load(ignlnkfilesPath)
				if err != nil {
					return fmt.Errorf("parsing .ignlnkfiles: %w", err)
				}
				newFiles, err = ignlnkfiles.DiscoverFiles(project.Root, ignorer, manifest)
				if err != nil {
					return fmt.Errorf("discovering files: %w", err)
				}
			}

			// Collect already-managed unlocked files
			var relock []string
			for relPath, entry := range manifest.Files {
				if entry.State == "unlocked" {
					relock = append(relock, relPath)
				}
			}

			allFiles := append(relock, newFiles...)

			if len(allFiles) == 0 {
				fmt.Println("nothing to lock")
				return nil
			}

			// Dry run mode
			if cmd.Bool("dry-run") {
				fmt.Println("files that would be locked:")
				for _, relPath := range allFiles {
					fmt.Printf("  %s\n", filepath.FromSlash(relPath))
				}
				return nil
			}

			cleanup := installSignalHandler(project, manifest)
			defer cleanup()

			force := cmd.Bool("force")
			newCount := 0
			relockCount := 0
			failed := 0

			for _, relPath := range allFiles {
				isNew := manifest.Files[relPath] == nil

				if err := core.LockFile(project, vault, manifest, relPath, force); err != nil {
					fmt.Fprintf(os.Stderr, "error: %s: %v\n", filepath.FromSlash(relPath), err)
					failed++
					continue
				}

				fmt.Printf("locked: %s\n", filepath.FromSlash(relPath))
				if isNew {
					newCount++
				} else {
					relockCount++
				}
			}

			if err := project.SaveManifest(manifest); err != nil {
				return fmt.Errorf("saving manifest: %w", err)
			}

			total := newCount + relockCount
			fmt.Printf("locked %d files (%d new, %d re-locked)\n", total, newCount, relockCount)

			if failed > 0 {
				return fmt.Errorf("%d files failed", failed)
			}
			return nil
		},
	}
}

func unlockAllCmd() *cli.Command {
	return &cli.Command{
		Name:  "unlock-all",
		Usage: "Unlock all managed files",
		Action: func(ctx context.Context, cmd *cli.Command) error {
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

			// Collect locked files
			var toUnlock []string
			for relPath, entry := range manifest.Files {
				if entry.State == "locked" {
					toUnlock = append(toUnlock, relPath)
				}
			}

			if len(toUnlock) == 0 {
				fmt.Println("nothing to unlock")
				return nil
			}

			cleanup := installSignalHandler(project, manifest)
			defer cleanup()

			succeeded := 0
			failed := 0

			for _, relPath := range toUnlock {
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

			fmt.Printf("unlocked %d files\n", succeeded)

			if failed > 0 {
				return fmt.Errorf("%d files failed", failed)
			}
			return nil
		},
	}
}
