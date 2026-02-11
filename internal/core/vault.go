package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/natefinch/atomic"
)

// Index represents ~/.ignlnk/index.json
type Index struct {
	Version  int                      `json:"version"`
	Projects map[string]*ProjectEntry `json:"projects"` // UID -> entry
}

// ProjectEntry maps a UID to a project root
type ProjectEntry struct {
	Root         string `json:"root"`
	RegisteredAt string `json:"registeredAt"`
}

// Vault represents a resolved vault for a specific project
type Vault struct {
	UID string // Short random hex ID
	Dir string // ~/.ignlnk/vault/<uid>/
}

// IgnlnkHome returns the path to ~/.ignlnk/, creating it if needed.
func IgnlnkHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	dir := filepath.Join(home, ".ignlnk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating ~/.ignlnk/: %w", err)
	}
	return dir, nil
}

// LockIndex acquires an exclusive file lock on ~/.ignlnk/index.lock.
func LockIndex() (unlock func(), err error) {
	home, err := IgnlnkHome()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(home, "index.lock")
	fl := flock.New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := fl.TryLockContext(ctx, 250*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("acquiring index lock: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("could not acquire index lock — another ignlnk operation may be running. If no other operation is active, delete ~/.ignlnk/index.lock and retry")
	}
	return func() { fl.Unlock() }, nil
}

// LoadIndex reads ~/.ignlnk/index.json. Returns an empty index if file doesn't exist.
func LoadIndex() (*Index, error) {
	home, err := IgnlnkHome()
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(home, "index.json")
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		return &Index{
			Version:  1,
			Projects: make(map[string]*ProjectEntry),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing index: %w", err)
	}
	if idx.Projects == nil {
		idx.Projects = make(map[string]*ProjectEntry)
	}
	return &idx, nil
}

// SaveIndex writes ~/.ignlnk/index.json atomically.
func SaveIndex(idx *Index) error {
	home, err := IgnlnkHome()
	if err != nil {
		return err
	}
	indexPath := filepath.Join(home, "index.json")
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index: %w", err)
	}
	data = append(data, '\n')
	r := strings.NewReader(string(data))
	if err := atomic.WriteFile(indexPath, r); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}
	return nil
}

// RegisterProject registers a project in the central index and creates its vault.
// If already registered, returns the existing vault.
func RegisterProject(projectRoot string) (*Vault, error) {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving project root: %w", err)
	}

	unlock, err := LockIndex()
	if err != nil {
		return nil, err
	}
	defer unlock()

	idx, err := LoadIndex()
	if err != nil {
		return nil, err
	}

	// Check if already registered
	for uid, entry := range idx.Projects {
		if normalizePath(entry.Root) == normalizePath(absRoot) {
			home, err := IgnlnkHome()
			if err != nil {
				return nil, err
			}
			return &Vault{
				UID: uid,
				Dir: filepath.Join(home, "vault", uid),
			}, nil
		}
	}

	// Generate new UID and register
	uid := generateUID()
	home, err := IgnlnkHome()
	if err != nil {
		return nil, err
	}
	vaultDir := filepath.Join(home, "vault", uid)
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating vault directory: %w", err)
	}

	idx.Projects[uid] = &ProjectEntry{
		Root:         absRoot,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveIndex(idx); err != nil {
		return nil, err
	}

	return &Vault{UID: uid, Dir: vaultDir}, nil
}

// ResolveVault looks up a project in the index and returns its vault.
func ResolveVault(projectRoot string) (*Vault, error) {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving project root: %w", err)
	}

	idx, err := LoadIndex()
	if err != nil {
		return nil, err
	}

	for uid, entry := range idx.Projects {
		if normalizePath(entry.Root) == normalizePath(absRoot) {
			home, err := IgnlnkHome()
			if err != nil {
				return nil, err
			}
			return &Vault{
				UID: uid,
				Dir: filepath.Join(home, "vault", uid),
			}, nil
		}
	}

	return nil, fmt.Errorf("project not registered — run 'ignlnk init' first")
}

// FilePath returns the OS-native vault path for a given manifest relative path.
func (v *Vault) FilePath(relPath string) string {
	return filepath.Join(v.Dir, filepath.FromSlash(relPath))
}

// BackupDir returns the path to the mirror backup vault (~/.ignlnk/vault/<uid>.backup/).
func (v *Vault) BackupDir() string {
	return filepath.Join(filepath.Dir(v.Dir), v.UID+".backup")
}

// BackupPath returns the backup path for a given manifest relative path.
func (v *Vault) BackupPath(relPath string) string {
	return filepath.Join(v.BackupDir(), filepath.FromSlash(relPath))
}

// generateUID generates 8 hex characters (4 random bytes) using crypto/rand.
func generateUID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// CheckSymlinkSupport tests whether symlinks work in the given directory.
func CheckSymlinkSupport(testDir string) error {
	testTarget := filepath.Join(testDir, ".ignlnk-symlink-test-target")
	testLink := filepath.Join(testDir, ".ignlnk-symlink-test-link")

	// Create a temporary target file
	if err := os.WriteFile(testTarget, []byte("test"), 0o644); err != nil {
		return fmt.Errorf("creating test file: %w", err)
	}
	defer os.Remove(testTarget)
	defer os.Remove(testLink)

	// Attempt to create symlink
	if err := os.Symlink(testTarget, testLink); err != nil {
		return fmt.Errorf("symlinks not supported: %w\nOn Windows, enable Developer Mode: Settings > Update & Security > For Developers", err)
	}

	return nil
}

// normalizePath normalizes a path for comparison (lowercase drive letter on Windows, clean).
func normalizePath(p string) string {
	p = filepath.Clean(p)
	// Normalize drive letter to uppercase on Windows
	if len(p) >= 2 && p[1] == ':' {
		p = strings.ToUpper(p[:1]) + p[1:]
	}
	return p
}
