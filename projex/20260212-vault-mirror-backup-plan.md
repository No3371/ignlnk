# Plan: Mirror Backup Vault

> **Status:** Ready
> **Created:** 2026-02-12
> **Source:** @20260212-vault-mirror-backup-proposal.md
> **Related Projex:** 20260212-vault-mirror-backup-proposal.md, 20260212-vault-redundancy-eval.md

---

## Summary

Add a sibling mirror backup vault `~/.ignlnk/vault/<uid>.backup/` that holds a single redundant copy of each vault file. Copy to backup on first lock (after vault copy verified); remove backup on forget. Fail lock if backup copy fails. Protects against vault corruption; forget removes both vault and backup.

**Scope:** internal/core (vault.go, fileops.go) — LockFile first-lock path, ForgetFile
**Estimated Changes:** 2 files, 2 functions, ~25 lines

---

## Objective

### Problem / Gap / Need

Single copy in vault → no recovery from corruption or accidental deletion. Proposal (20260212-vault-mirror-backup-proposal.md) specifies sibling mirror dir, single redundant file per path, forget removes backup.

### Success Criteria

- [ ] `Vault.BackupPath(relPath)` and `Vault.BackupDir()` return correct paths
- [ ] First lock copies to vault and to mirror backup; lock fails if backup copy fails
- [ ] Forget removes both vault file and backup; cleans empty backup parents
- [ ] Re-lock path unchanged (no backup write)
- [ ] `go build ./...` and `go vet ./...` pass

### Out of Scope

- README / docs update (separate change)
- Automated tests (separate plan)
- Recovery tooling (manual copy from backup)
- Backup root creation at RegisterProject (lazy on first lock)

---

## Context

### Current State

- **vault.go:** `Vault` has `UID`, `Dir`. `FilePath(relPath)` returns `vault/<uid>/<relPath>`.
- **fileops.go LockFile:** First-lock path (L111–144): copy to vault, verify hash, write placeholder, update manifest. No backup.
- **fileops.go ForgetFile:** Copy vault→project, remove vault file, `removeEmptyParents` to vault root. No backup removal.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/vault.go | Vault path helpers | Add BackupDir, BackupPath |
| internal/core/fileops.go | Lock/unlock/forget | LockFile: copy to backup; ForgetFile: remove backup |

### Dependencies

- **Requires:** None
- **Blocks:** None

### Constraints

- Backup copy failure → fail lock (data integrity per proposal)
- Forget idempotent if backup missing (legacy vaults)
- Lazy backup root creation (no RegisterProject change)

---

## Implementation

### Overview

1. Add `BackupDir()` and `BackupPath(relPath)` to Vault
2. In LockFile first-lock path: after vault verified, copy to backup; on failure remove vault and return
3. In ForgetFile: remove backup path, cleanup backup parents

---

### Step 1: Add Vault backup path helpers

**Objective:** Expose backup root and per-file backup path.

**Files:**
- internal/core/vault.go

**Changes:**

```go
// After FilePath (around L201), add:

// BackupDir returns the path to the mirror backup vault (~/.ignlnk/vault/<uid>.backup/).
func (v *Vault) BackupDir() string {
	return filepath.Join(filepath.Dir(v.Dir), v.UID+".backup")
}

// BackupPath returns the backup path for a given manifest relative path.
func (v *Vault) BackupPath(relPath string) string {
	return filepath.Join(v.BackupDir(), filepath.FromSlash(relPath))
}
```

**Rationale:** BackupDir used for removeEmptyParents stop boundary; BackupPath for per-file operations. Sibling layout survives `rm -r vault/<uid>/`.

**Verification:** `go build ./...` succeeds; new methods compile.

---

### Step 2: Copy to backup in LockFile first-lock path

**Objective:** After vault copy verified, copy to backup. Fail lock if backup copy fails.

**Files:**
- internal/core/fileops.go

**Changes:**

Insert after the vault hash verification block (after L128, before "Point of no return" comment), before writing placeholder:

```go
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
```

**Rationale:** Backup must succeed or we roll back vault copy. Same content as vault (already verified).

**Verification:** Lock a file → backup exists at `vault/<uid>.backup/<relPath>`; intentionally break backup path (e.g., permissions) → lock fails, vault file removed.

---

### Step 3: Remove backup in ForgetFile

**Objective:** Remove backup file and empty backup parents when forgetting.

**Files:**
- internal/core/fileops.go

**Changes:**

In ForgetFile, after `os.Remove(vaultPath)` and `removeEmptyParents` for vault (L229–230), add:

```go
	// Remove vault file and empty parent dirs
	os.Remove(vaultPath)
	removeEmptyParents(filepath.Dir(vaultPath), vault.Dir)

	// Remove backup and empty backup parents
	backupPath := vault.BackupPath(relPath)
	os.Remove(backupPath)
	removeEmptyParents(filepath.Dir(backupPath), vault.BackupDir())
```

**Rationale:** Idempotent — os.Remove on missing file returns error we ignore. removeEmptyParents stops at backup root.

**Verification:** Forget a file → backup path removed; forget on file locked before this change (no backup) → no error.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

- [ ] `ignlnk init` in temp dir; `ignlnk lock .env` (create .env first) → `~/.ignlnk/vault/<uid>/.env` and `~/.ignlnk/vault/<uid>.backup/.env` exist
- [ ] `ignlnk forget .env` → both vault and backup paths removed
- [ ] Lock file, chmod 000 backup parent dir, run lock on another file → fails with backup error
- [ ] Existing project with locked files (no backup) → forget works without error

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| BackupPath/BackupDir correct | Inspect paths for uid abc12345 | `.../vault/abc12345.backup/relPath` |
| First lock creates backup | Lock new file, ls backup dir | File present |
| Backup failure fails lock | Block backup write, lock | Error, vault file removed |
| Forget removes backup | Forget, ls backup dir | File gone, empty dirs pruned |
| Re-lock unchanged | Unlock, lock → no backup write | Backup unchanged (still from first lock) |

---

## Rollback Plan

1. Revert commit
2. Existing vaults unchanged; new backup dirs may remain as orphan dirs (user can delete `~/.ignlnk/vault/*.backup` manually)

---

## Notes

### Assumptions

- Manifest lock held during LockFile/ForgetFile
- copyFile creates parent dirs (it does)
- removeEmptyParents handles nested empty dirs correctly

### Risks

- **Orphan backup dirs:** If process dies between vault write and backup write, vault has file but no backup. Acceptable.
- **Backup out of sync:** Manual edit to vault; backup stale. User responsibility.

### Open Questions

- None
