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
	v := &Vault{UID: "test", Dir: vaultDir}
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

func TestUnlockFilePlaceholderSuccess(t *testing.T) {
	if err := CheckSymlinkSupport(t.TempDir()); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)

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

	// Write placeholder at absPath
	placeholder := GeneratePlaceholder(relPath)
	if err := os.WriteFile(absPath, placeholder, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: hash}

	err = UnlockFile(p, v, m, relPath)
	if err != nil {
		t.Fatalf("UnlockFile failed: %v", err)
	}

	entry := m.Files[relPath]
	if entry == nil || entry.State != "unlocked" {
		t.Fatalf("expected state unlocked, got %v", entry)
	}

	// Verify absPath is now symlink
	info, err := os.Lstat(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink at absPath")
	}
}

func TestUnlockFileRegularFileRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)
	content := []byte("user data - do not lose")

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, []byte("vault content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create regular file (not placeholder) at absPath
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := UnlockFile(p, v, m, relPath)
	if err == nil {
		t.Fatal("expected UnlockFile to return error for regular file, got nil")
	}

	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content changed: got %q, want %q", string(got), string(content))
	}
}

func TestForgetFilePlaceholderSuccess(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)
	content := []byte("secret data")

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	placeholder := GeneratePlaceholder(relPath)
	if err := os.WriteFile(absPath, placeholder, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := ForgetFile(p, v, m, relPath)
	if err != nil {
		t.Fatalf("ForgetFile failed: %v", err)
	}

	if _, ok := m.Files[relPath]; ok {
		t.Fatal("expected entry removed from manifest")
	}

	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected restored content %q, got %q", string(content), string(got))
	}

	if _, err := os.Stat(vaultPath); !os.IsNotExist(err) {
		t.Fatal("expected vault file removed")
	}
}

func TestForgetFileSymlinkSuccess(t *testing.T) {
	if err := CheckSymlinkSupport(t.TempDir()); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)
	content := []byte("secret data")

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(vaultPath, absPath); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "unlocked", Hash: "sha256:fake"}

	err := ForgetFile(p, v, m, relPath)
	if err != nil {
		t.Fatalf("ForgetFile failed: %v", err)
	}

	if _, ok := m.Files[relPath]; ok {
		t.Fatal("expected entry removed from manifest")
	}

	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected restored content %q, got %q", string(content), string(got))
	}
}

func TestForgetFileRegularFileRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)
	content := []byte("user data - do not lose")

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	vaultContent := []byte("vault content")
	if err := os.WriteFile(vaultPath, vaultContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create regular file (not placeholder) at absPath
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := ForgetFile(p, v, m, relPath)
	if err == nil {
		t.Fatal("expected ForgetFile to return error for regular file, got nil")
	}

	// Verify file unchanged
	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content changed: got %q, want %q", string(got), string(content))
	}

	// Verify vault unchanged
	vGot, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(vGot) != string(vaultContent) {
		t.Fatalf("vault content changed: got %q, want %q", string(vGot), string(vaultContent))
	}
}

func TestUnlockFilePlaceholderWithAppendedContentRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, []byte("vault content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Placeholder + appended content — size spoof; must be refused
	placeholder := GeneratePlaceholder(relPath)
	content := append(placeholder, []byte("extra user content")...)
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := UnlockFile(p, v, m, relPath)
	if err == nil {
		t.Fatal("expected UnlockFile to return error for placeholder with appended content, got nil")
	}

	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content changed: got %q, want %q", string(got), string(content))
	}
}

func TestUnlockFileDirectoryRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)

	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, []byte("vault content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create directory at absPath (where file is expected)
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := UnlockFile(p, v, m, relPath)
	if err == nil {
		t.Fatal("expected UnlockFile to return error for directory, got nil")
	}

	// Verify directory still exists
	info, err := os.Stat(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory at absPath to remain")
	}
}

func TestForgetFilePlaceholderWithAppendedContentRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)
	vaultContent := []byte("vault content")

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, vaultContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Placeholder + appended content — size spoof; must be refused
	placeholder := GeneratePlaceholder(relPath)
	content := append(placeholder, []byte("extra user content")...)
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := ForgetFile(p, v, m, relPath)
	if err == nil {
		t.Fatal("expected ForgetFile to return error for placeholder with appended content, got nil")
	}

	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content changed: got %q, want %q", string(got), string(content))
	}
	if vGot, _ := os.ReadFile(vaultPath); string(vGot) != string(vaultContent) {
		t.Fatal("vault content should be unchanged")
	}
}

func TestForgetFileDirectoryRefused(t *testing.T) {
	p, v, m, cleanup := setupLockFileTest(t)
	defer cleanup()

	relPath := "config/secret.txt"
	absPath := p.AbsPath(relPath)
	vaultPath := v.FilePath(relPath)
	vaultContent := []byte("vault content")

	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultPath, vaultContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create directory at absPath (where file is expected)
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		t.Fatal(err)
	}

	m.Files[relPath] = &FileEntry{State: "locked", Hash: "sha256:fake"}

	err := ForgetFile(p, v, m, relPath)
	if err == nil {
		t.Fatal("expected ForgetFile to return error for directory, got nil")
	}

	// Verify directory still exists
	info, err := os.Stat(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory at absPath to remain")
	}
	if vGot, _ := os.ReadFile(vaultPath); string(vGot) != string(vaultContent) {
		t.Fatal("vault content should be unchanged")
	}
}
