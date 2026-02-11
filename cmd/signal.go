package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/user/ignlnk/internal/core"
)

// installSignalHandler registers a SIGINT handler that saves the manifest before exit.
// Returns a cleanup function to deregister the handler.
func installSignalHandler(project *core.Project, manifest *core.Manifest) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			fmt.Fprintln(os.Stderr, "\ninterrupted — saving manifest...")
			if err := project.SaveManifest(manifest); err != nil {
				fmt.Fprintf(os.Stderr, "error saving manifest: %v\n", err)
			}
			os.Exit(1)
		case <-done:
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}
