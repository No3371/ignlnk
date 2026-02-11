package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupLockFileTest creates a temp dir with .ignlnk, vault, manifest.
// Returns project, vault, manifest, cleanup func.
func setupLockFileTest(t *testing.T) (*Project, *Vault, *Manifest, func()) {
	t.Helper()
	tmp := t.TempDir()
	ignlnkDir := filepath.Join(tmp, ".ignlnk")
	if err := os.MkdirAll(ignlnkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	vaultDir := filepath.Join(ignlnkDir, "vault")
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := &Project{Root: tmp, IgnlnkDir: ignlnkDir}
	v := &Vault{Dir: vaultDir}
	m := &Manifest{Version: 1, Files: make(map[string]*FileEntry)}
	return p, v, m, func() {}
}

func TestLockFileRelockSymlink(t *testing.T) {
	if err := CheckSymlinkSupport(t.TempDir()); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)

	// Create parent dir and vault file
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("secret data")
	if err := os.WriteFile(vaultPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create symlink at absPath pointing to vault
	if err := os.Symlink(vaultPath, absPath); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "unlocked", Hash: hash}

	err = LockFile(p, v, m, relPath, false)
	if err != nil {
		t.Fatalf("LockFile failed: %v", err)
	}

	entry := m.Files[relPath]
	if entry == nil || entry.State != "locked" {
		t.Fatalf("expected state locked, got %v", entry)
	}

	// Verify absPath is now placeholder
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), placeholderPrefix) {
		t.Fatalf("expected placeholder at absPath, got: %s", string(data))
	}
}

func TestLockFileRelockRegularFileRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	content := []byte("user data - do not lose")

	// Create regular file at absPath
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "unlocked", Hash: "sha256:fake"}

	err := LockFile(p, v, m, relPath, false)
	if err == nil {
		t.Fatal("expected LockFile to return error for regular file, got nil")
	}

	// Verify file unchanged
	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content changed: got %q, want %q", string(got), string(content))
	}
}
