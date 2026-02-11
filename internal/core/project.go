package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/natefinch/atomic"
)

// Manifest represents .ignlnk/manifest.json
type Manifest struct {
	Version int                   `json:"version"`
	Files   map[string]*FileEntry `json:"files"`
}

// FileEntry represents a single managed file
type FileEntry struct {
	State    string `json:"state"`    // "locked" or "unlocked"
	LockedAt string `json:"lockedAt"` // ISO 8601 timestamp
	Hash     string `json:"hash"`     // "sha256:<hex>"
}

// Project represents a detected ignlnk project
type Project struct {
	Root         string // Absolute path to project root
	IgnlnkDir    string // Absolute path to .ignlnk/
	ManifestPath string // Absolute path to .ignlnk/manifest.json
}

// FindProject walks up from startDir looking for a .ignlnk/ directory.
func FindProject(startDir string) (*Project, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	for {
		candidate := filepath.Join(dir, ".ignlnk")
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return &Project{
				Root:         dir,
				IgnlnkDir:    candidate,
				ManifestPath: filepath.Join(candidate, "manifest.json"),
			}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("not an ignlnk project (no .ignlnk/ found in %s or any parent)", startDir)
		}
		dir = parent
	}
}

// InitProject creates .ignlnk/ and an empty manifest.json in dir.
func InitProject(dir string) (*Project, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	ignlnkDir := filepath.Join(absDir, ".ignlnk")
	if err := os.MkdirAll(ignlnkDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating .ignlnk/: %w", err)
	}
	p := &Project{
		Root:         absDir,
		IgnlnkDir:    ignlnkDir,
		ManifestPath: filepath.Join(ignlnkDir, "manifest.json"),
	}
	m := &Manifest{
		Version: 1,
		Files:   make(map[string]*FileEntry),
	}
	if err := p.SaveManifest(m); err != nil {
		return nil, fmt.Errorf("writing initial manifest: %w", err)
	}
	return p, nil
}

// LoadManifest reads and parses manifest.json.
func (p *Project) LoadManifest() (*Manifest, error) {
	data, err := os.ReadFile(p.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = make(map[string]*FileEntry)
	}
	return &m, nil
}

// SaveManifest writes manifest.json atomically.
func (p *Project) SaveManifest(m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	data = append(data, '\n')
	r := strings.NewReader(string(data))
	if err := atomic.WriteFile(p.ManifestPath, r); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// LockManifest acquires an exclusive file lock on .ignlnk/manifest.lock.
// Returns an unlock function for defer. Times out after 30 seconds.
func (p *Project) LockManifest() (unlock func(), err error) {
	lockPath := filepath.Join(p.IgnlnkDir, "manifest.lock")
	fl := flock.New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := fl.TryLockContext(ctx, 250*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("acquiring manifest lock: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("could not acquire lock — another ignlnk operation may be running. If no other operation is active, delete .ignlnk/manifest.lock and retry")
	}
	return func() { fl.Unlock() }, nil
}

// RelPath converts an absolute path to a project-relative, forward-slash-normalized path.
func (p *Project) RelPath(absPath string) (string, error) {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	rel, err := filepath.Rel(p.Root, abs)
	if err != nil {
		return "", fmt.Errorf("computing relative path: %w", err)
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %s is outside the project root", absPath)
	}
	return rel, nil
}

// AbsPath converts a forward-slash relative path to an OS-native absolute path.
func (p *Project) AbsPath(relPath string) string {
	return filepath.Join(p.Root, filepath.FromSlash(relPath))
}
