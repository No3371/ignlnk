package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/urfave/cli/v3"
	"github.com/user/ignlnk/internal/core"
)

func listCmd() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all managed files",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			project, err := core.FindProject(".")
			if err != nil {
				return err
			}

			// No manifest lock — read-only command
			manifest, err := project.LoadManifest()
			if err != nil {
				return err
			}

			if len(manifest.Files) == 0 {
				fmt.Println("no managed files")
				return nil
			}

			keys := make([]string, 0, len(manifest.Files))
			for k := range manifest.Files {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, relPath := range keys {
				fmt.Println(filepath.FromSlash(relPath))
			}
			return nil
		},
	}
}
