package cmd

import "github.com/urfave/cli/v3"

// NewApp creates the root ignlnk CLI command with all subcommands.
func NewApp() *cli.Command {
	return &cli.Command{
		Name:  "ignlnk",
		Usage: "Protect sensitive files from AI coding agents",
		Commands: []*cli.Command{
			initCmd(),
			lockCmd(),
			unlockCmd(),
			statusCmd(),
			listCmd(),
			forgetCmd(),
			lockAllCmd(),
			unlockAllCmd(),
		},
	}
}
