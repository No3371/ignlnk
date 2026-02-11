# Plan: ForgetFile — verify path before remove

> **Status:** Ready
> **Created:** 2026-02-12
> **Source:** @20260212-fileops-lockfile-relock-redteam.md (Should Fix — ForgetFile)
> **Related Projex:** 20260212-fileops-lockfile-relock-redteam.md

---

## Summary

Add verification in ForgetFile before `os.Remove` so that we only proceed when the path is a symlink or an ignlnk placeholder. If the path holds a regular file that is not a placeholder (e.g. user copied real content from vault, overwriting the symlink), refuse and return a clear error. Prevents overwriting and loss of user data.

**Scope:** internal/core/fileops.go ForgetFile (L214–218)
**Estimated Changes:** 1 file, 1 function, ~12 lines

---

## Objective

### Problem / Gap / Need

ForgetFile removes whatever is at the project path, copies vault content back, and deletes the vault copy. If the user overwrote the symlink with a real file (e.g. copied from vault and made edits), the code blindly removes that file. User edits are lost; the vault copy (which may be stale) is restored and then the vault is deleted. No recovery.

### Success Criteria

- [ ] Forget when path is a placeholder succeeds (unchanged behavior)
- [ ] Forget when path is a symlink succeeds (unchanged behavior)
- [ ] Forget when path is a regular file and NOT a placeholder returns an error and does NOT remove the file or vault
- [ ] Error message clearly explains the situation and suggests remediation

### Out of Scope

- Adding `--force` to forget command (future consideration)
- Adding automated tests (separate plan)
- Path missing (create dirs and copy; current behavior)

---

## Context

### Current State

```go
// Remove whatever is at the original path (placeholder or symlink)
if _, err := os.Lstat(absPath); err == nil {
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing existing file: %w", err)
    }
}
```

No verification that the path is placeholder or symlink. Regular file with user data is removed.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/fileops.go | ForgetFile | Add verification; refuse if regular file and !IsPlaceholder |

### Dependencies

- **Requires:** None
- **Blocks:** None (tests can be added separately)

### Constraints

- Must preserve forget semantics when path is placeholder (locked) or symlink (unlocked)
- Error must be actionable (user can lock first, or manually reconcile)

---

## Implementation

### Overview

Before `os.Remove`, use `os.Lstat` and verify: path must be either (1) a symlink, or (2) a regular file that `IsPlaceholder` returns true for. If path is a regular file and not a placeholder, return an error and refuse.

### Step 1: Verify path type before remove

**Objective:** Prevent removal of user data when path holds a real file that is not a placeholder.

**Files:**
- internal/core/fileops.go

**Changes:**

```go
// Remove whatever is at the original path (placeholder or symlink)
// Verify path is expected type before destructive operation
if info, err := os.Lstat(absPath); err == nil {
    if info.Mode().IsRegular() && !IsPlaceholder(absPath) {
        return fmt.Errorf("refusing to forget %s: path contains user data (not a placeholder or symlink). Run 'ignlnk lock %s' first to lock, then forget", relPath, relPath)
    }
    // Path is symlink or placeholder — safe to remove
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing existing file: %w", err)
    }
}
```

**Rationale:** Lstat + IsPlaceholder verifies filesystem state. Refusing when regular file and !IsPlaceholder prevents overwriting user edits. Error suggests locking first (which will fail or handle divergence via other plans).

**Verification:** Manual test: (1) unlock file, replace symlink with regular file with edits, run forget → must error, file and vault unchanged; (2) unlock file, leave symlink, run forget → must succeed; (3) lock file, leave placeholder, run forget → must succeed.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

- [ ] Forget with placeholder at path → succeeds
- [ ] Forget with symlink at path → succeeds
- [ ] Forget with regular file (non-placeholder) at path → fails with clear error, file and vault unchanged

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Placeholder forget succeeds | Lock file, run forget | File restored, vault removed |
| Symlink forget succeeds | Unlock file, run forget | File restored, vault removed |
| Non-placeholder refused | Put real file at path, run forget | Error returned, nothing removed |

---

## Rollback Plan

If the change causes regressions: revert the commit. The previous behavior (blind remove) is unsafe but known; rollback restores that until a corrected fix is applied.

---

## Notes

### Assumptions

- Manifest lock is held when ForgetFile is called
- `IsPlaceholder` correctly identifies ignlnk placeholders
- Path missing: Lstat returns err != nil; we skip remove and proceed to copy. No change to that flow.

### Risks

- Error message suggests `ignlnk lock` first; if path has user edits, lock would fail (regular file, not placeholder). User may need to manually copy content to vault path, then lock, then forget. Document in error or separate help. For this plan, error is sufficient.
