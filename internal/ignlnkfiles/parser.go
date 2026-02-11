package ignlnkfiles

import (
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/user/ignlnk/internal/core"
)

// Load reads a .ignlnkfiles file and compiles the patterns.
func Load(path string) (*ignore.GitIgnore, error) {
	return ignore.CompileIgnoreFile(path)
}

// DiscoverFiles walks the project tree and returns all files matching .ignlnkfiles patterns.
// Excludes .ignlnk/ directory and already-managed files.
func DiscoverFiles(projectRoot string, ignorer *ignore.GitIgnore, manifest *core.Manifest) ([]string, error) {
	var matches []string

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute relative path (forward-slash normalized)
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Skip dotdirs
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}

		// Only regular files
		if !d.Type().IsRegular() {
			return nil
		}

		// Skip already-managed files
		if _, ok := manifest.Files[rel]; ok {
			return nil
		}

		// Check against patterns
		if ignorer.MatchesPath(rel) {
			matches = append(matches, rel)
		}

		return nil
	})

	return matches, err
}
