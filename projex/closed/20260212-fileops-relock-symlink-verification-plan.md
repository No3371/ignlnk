# Plan: LockFile re-lock — symlink verification before remove

> **Status:** Merged
> **Merged into:** 20260212-fileops-verify-path-before-remove-plan.md
> **Created:** 2026-02-12
> **Source:** @20260212-fileops-lockfile-relock-redteam.md (Must Fix)
> **Related Projex:** 20260212-fileops-lockfile-relock-redteam.md

---

## Summary

Add an `os.Lstat(absPath)` check before `os.Remove` in the LockFile re-lock path. If the path is not a symlink, refuse and return a clear error to prevent blind removal of user data when manifest and filesystem diverge.

**Scope:** internal/core/fileops.go re-lock branch (L73–85)
**Estimated Changes:** 1 file, 1 function, ~8 lines

---

## Objective

### Problem / Gap / Need

The re-lock path (manifest says "unlocked") assumes absPath is a symlink and calls `os.Remove(absPath)` without verification. If the user replaced the symlink with a regular file (e.g., copied from vault), or the manifest is corrupted, the code deletes the real file and writes a placeholder — causing data loss.

### Success Criteria

- [ ] Re-lock when absPath is a symlink succeeds (unchanged behavior)
- [ ] Re-lock when absPath is a regular file returns an error and does NOT remove the file
- [ ] Error message clearly explains the situation and suggests remediation

### Out of Scope

- Adding automated tests (separate plan)
- Divergence logging (separate plan)
- Repair tooling

---

## Context

### Current State

```go
// Re-locking: if file is already managed and unlocked (symlink), just swap symlink for placeholder
if entry, ok := manifest.Files[relPath]; ok && entry.State == "unlocked" {
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing symlink: %w", err)
    }
    // ... write placeholder
}
```

No verification that absPath is a symlink before removal.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/fileops.go | LockFile re-lock branch | Add Lstat, refuse if not symlink |

### Dependencies

- **Requires:** None
- **Blocks:** 20260212-fileops-relock-tests-plan.md (tests will verify this fix)

### Constraints

- Must preserve idempotent and re-lock semantics when path is valid symlink
- Error must be actionable (user can fix by unlocking first, then locking)

---

## Implementation

### Overview

Insert an `os.Lstat(absPath)` before `os.Remove`. If the path does not exist, or is not a symlink, return a descriptive error. Add a brief comment documenting the defensive check.

### Step 1: Add symlink verification before remove

**Objective:** Verify absPath is a symlink before removing; refuse if not.

**Files:**
- internal/core/fileops.go

**Changes:**

```go
// Re-locking: if file is already managed and unlocked (symlink), just swap symlink for placeholder
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
```

**Rationale:** Lstat verifies filesystem state before destructive operation. Refusing when not symlink prevents data loss. Error message guides user to unlock first (which will fail or restore state) then lock.

**Verification:** Manual test: (1) unlock a file, replace symlink with regular file, run lock → must error; (2) unlock a file, leave symlink, run lock → must succeed.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

- [ ] Re-lock with symlink present → succeeds
- [ ] Re-lock with regular file at path → fails with clear error, file unchanged

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Symlink re-lock succeeds | Unlock file, run lock | Placeholder written, manifest updated |
| Non-symlink refused | Put regular file at path, run lock | Error returned, file not removed |

---

## Rollback Plan

If the change causes regressions: revert the commit. The previous behavior (blind remove) is unsafe but known; rollback restores that until a corrected fix is applied.

---

## Notes

### Assumptions

- Manifest lock (LockManifest) is held when LockFile is called, reducing concurrent modification risk
- User has symlink support (already checked earlier in unlock flow)

### Risks

- Error message length: may be long; acceptable for safety-critical path
