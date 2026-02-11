package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/natefinch/atomic"
)

const (
	placeholderPrefix = "[ignlnk:protected]"
	largeSizeWarning  = 100 * 1024 * 1024  // 100MB
	largeSizeLimit    = 1024 * 1024 * 1024  // 1GB
	progressThreshold = 10 * 1024 * 1024    // 10MB
)

var (
	symlinkChecked bool
	symlinkOK      bool
	symlinkMu      sync.Mutex
)

// HashFile computes SHA-256 of a file, returns "sha256:<hex>".
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file for hash: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat for hash: %w", err)
	}

	h := sha256.New()
	if info.Size() > progressThreshold {
		fmt.Fprintf(os.Stderr, "hashing %s (%d MB)...\n", filepath.Base(path), info.Size()/(1024*1024))
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// GeneratePlaceholder returns placeholder content for a given relative path.
func GeneratePlaceholder(relPath string) []byte {
	content := fmt.Sprintf(`%s This file is protected by ignlnk.
To view its contents, ask the user to run:

    ignlnk unlock %s

Do NOT attempt to modify or bypass this file.
`, placeholderPrefix, relPath)
	return []byte(content)
}

// LockFile moves a file to the vault and replaces it with a placeholder.
func LockFile(project *Project, vault *Vault, manifest *Manifest, relPath string, force bool) error {
	// Idempotent: already locked = no-op
	if entry, ok := manifest.Files[relPath]; ok && entry.State == "locked" {
		return nil
	}

	absPath := project.AbsPath(relPath)

	// Re-locking: if file is already managed and unlocked (symlink), swap symlink for placeholder.
	// We verify absPath is a symlink before removing — if it's a regular file, refuse to avoid data loss.
	if entry, ok := manifest.Files[relPath]; ok && entry.State == "unlocked" {
		info, err := os.Lstat(absPath)
		if err != nil {
			return fmt.Errorf("stat before re-lock: %w", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to re-lock %s: path is not a symlink (may contain user data). Run 'ignlnk unlock %s' first, then lock again", relPath, relPath)
		}
		if err := os.Remove(absPath); err != nil {
			return fmt.Errorf("removing symlink: %w", err)
		}
		placeholder := GeneratePlaceholder(relPath)
		r := strings.NewReader(string(placeholder))
		if err := atomic.WriteFile(absPath, r); err != nil {
			return fmt.Errorf("writing placeholder: %w", err)
		}
		entry.State = "locked"
		return nil
	}

	// Verify file exists and is regular
	info, err := os.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", relPath)
	}

	// Size checks
	size := info.Size()
	if size > largeSizeLimit && !force {
		return fmt.Errorf("file exceeds 1GB (%d MB), use --force to lock large files", size/(1024*1024))
	}
	if size > largeSizeWarning {
		fmt.Fprintf(os.Stderr, "warning: large file (%d MB): %s\n", size/(1024*1024), filepath.FromSlash(relPath))
	}

	// Hash the original file
	hash, err := HashFile(absPath)
	if err != nil {
		return err
	}

	// Create vault parent dirs and copy to vault
	vaultPath := vault.FilePath(relPath)
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}
	if err := copyFile(absPath, vaultPath); err != nil {
		return fmt.Errorf("copying to vault: %w", err)
	}

	// Verify vault copy hash
	vaultHash, err := HashFile(vaultPath)
	if err != nil {
		os.Remove(vaultPath)
		return fmt.Errorf("verifying vault copy: %w", err)
	}
	if vaultHash != hash {
		os.Remove(vaultPath)
		return fmt.Errorf("vault copy hash mismatch — aborting lock")
	}

	// Copy to mirror backup (single redundant copy; fail lock if backup fails)
	backupPath := vault.BackupPath(relPath)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		os.Remove(vaultPath)
		return fmt.Errorf("creating backup directory: %w", err)
	}
	if err := copyFile(vaultPath, backupPath); err != nil {
		os.Remove(vaultPath)
		return fmt.Errorf("copying to backup vault: %w", err)
	}

	// Point of no return: vault copy verified. Write placeholder over original.
	placeholder := GeneratePlaceholder(relPath)
	r := strings.NewReader(string(placeholder))
	if err := atomic.WriteFile(absPath, r); err != nil {
		return fmt.Errorf("writing placeholder: %w", err)
	}

	// Update manifest entry
	manifest.Files[relPath] = &FileEntry{
		State:    "locked",
		LockedAt: time.Now().UTC().Format(time.RFC3339),
		Hash:     hash,
	}
	return nil
}

// UnlockFile replaces a placeholder with a symlink to the vault copy.
func UnlockFile(project *Project, vault *Vault, manifest *Manifest, relPath string) error {
	// Symlink capability check (cached)
	if err := ensureSymlinkSupport(project.IgnlnkDir); err != nil {
		return err
	}

	// Idempotent: already unlocked = no-op
	if entry, ok := manifest.Files[relPath]; ok && entry.State == "unlocked" {
		return nil
	}

	// Must be managed
	entry, ok := manifest.Files[relPath]
	if !ok {
		return fmt.Errorf("file not managed: %s", relPath)
	}

	// Verify vault file exists
	vaultPath := vault.FilePath(relPath)
	if _, err := os.Stat(vaultPath); err != nil {
		return fmt.Errorf("vault file missing: %w", err)
	}

	// Verify vault file hash
	vaultHash, err := HashFile(vaultPath)
	if err != nil {
		return fmt.Errorf("verifying vault file: %w", err)
	}
	if vaultHash != entry.Hash {
		fmt.Fprintf(os.Stderr, "warning: vault file hash mismatch for %s\n", filepath.FromSlash(relPath))
	}

	absPath := project.AbsPath(relPath)

	// Check if placeholder exists and is actually a placeholder
	if info, err := os.Lstat(absPath); err == nil {
		if info.Mode().IsRegular() && !IsPlaceholder(absPath) {
			fmt.Fprintf(os.Stderr, "warning: file at %s has been modified (not a placeholder)\n", filepath.FromSlash(relPath))
		}
		// Remove the placeholder (or whatever is there)
		if err := os.Remove(absPath); err != nil {
			return fmt.Errorf("removing placeholder: %w", err)
		}
	}

	// Create symlink: original path -> vault absolute path
	if err := os.Symlink(vaultPath, absPath); err != nil {
		return fmt.Errorf("creating symlink: %w", err)
	}

	// Update manifest
	entry.State = "unlocked"
	return nil
}

// ForgetFile restores a file from vault and removes it from management.
func ForgetFile(project *Project, vault *Vault, manifest *Manifest, relPath string) error {
	entry, ok := manifest.Files[relPath]
	if !ok {
		return fmt.Errorf("file not managed: %s", relPath)
	}

	absPath := project.AbsPath(relPath)
	vaultPath := vault.FilePath(relPath)

	// Remove whatever is at the original path (placeholder or symlink)
	if _, err := os.Lstat(absPath); err == nil {
		if err := os.Remove(absPath); err != nil {
			return fmt.Errorf("removing existing file: %w", err)
		}
	}

	// Copy vault file back to original location
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}
	if err := copyFile(vaultPath, absPath); err != nil {
		return fmt.Errorf("restoring file from vault: %w", err)
	}

	// Remove vault file and empty parent dirs
	os.Remove(vaultPath)
	removeEmptyParents(filepath.Dir(vaultPath), vault.Dir)

	// Remove backup and empty backup parents
	backupPath := vault.BackupPath(relPath)
	os.Remove(backupPath)
	removeEmptyParents(filepath.Dir(backupPath), vault.BackupDir())

	// Remove from manifest (in-memory; caller saves)
	delete(manifest.Files, relPath)
	_ = entry // suppress unused warning
	return nil
}

// IsPlaceholder checks if a file at the given path is an ignlnk placeholder.
func IsPlaceholder(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(placeholderPrefix))
	n, err := f.Read(buf)
	if err != nil || n < len(placeholderPrefix) {
		return false
	}
	return string(buf) == placeholderPrefix
}

// FileStatus returns the actual filesystem state of a managed file.
func FileStatus(project *Project, vault *Vault, entry *FileEntry, relPath string) string {
	absPath := project.AbsPath(relPath)
	vaultPath := vault.FilePath(relPath)

	// Check vault file exists
	_, vaultErr := os.Stat(vaultPath)
	if vaultErr != nil {
		return "missing"
	}

	// Check original path
	info, err := os.Lstat(absPath)
	if err != nil {
		return "unknown"
	}

	// Symlink = unlocked state
	if info.Mode()&os.ModeSymlink != 0 {
		// Check if vault file has been modified
		hash, err := HashFile(vaultPath)
		if err == nil && hash != entry.Hash {
			return "dirty"
		}
		return "unlocked"
	}

	// Regular file = should be placeholder
	if info.Mode().IsRegular() {
		if IsPlaceholder(absPath) {
			return "locked"
		}
		return "tampered"
	}

	return "unknown"
}

// copyFile copies src to dst, creating dst's parent directories.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// removeEmptyParents removes empty directories from dir up to (but not including) stopAt.
func removeEmptyParents(dir, stopAt string) {
	for dir != stopAt && dir != filepath.Dir(dir) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// ensureSymlinkSupport checks symlink capability once per session.
func ensureSymlinkSupport(testDir string) error {
	symlinkMu.Lock()
	defer symlinkMu.Unlock()
	if symlinkChecked {
		if !symlinkOK {
			return fmt.Errorf("symlinks not supported on this system. On Windows, enable Developer Mode: Settings > Update & Security > For Developers")
		}
		return nil
	}
	symlinkChecked = true
	if err := CheckSymlinkSupport(testDir); err != nil {
		symlinkOK = false
		return err
	}
	symlinkOK = true
	return nil
}
