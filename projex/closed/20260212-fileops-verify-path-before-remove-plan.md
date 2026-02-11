# Plan: fileops — verify path type before remove (LockFile, UnlockFile, ForgetFile)

> **Status:** Complete
> **Completed:** 2026-02-12
> **Walkthrough:** 20260212-fileops-verify-path-before-remove-walkthrough.md
> **Created:** 2026-02-12
> **Source:** @20260212-fileops-lockfile-relock-redteam.md (Must Fix + Should Fix)
> **Related Projex:** 20260212-fileops-lockfile-relock-redteam.md
> **Merged from:** 20260212-fileops-relock-symlink-verification-plan.md, 20260212-fileops-forget-path-verification-plan.md, 20260212-fileops-unlock-placeholder-verification-plan.md

---

## Summary

Add verification in `fileops.go` before any `os.Remove` that could destroy user data. Three paths: (1) LockFile re-lock — verify path is a symlink before removing; (2) UnlockFile — verify path is placeholder or symlink before removing (refuse when regular file with user data); (3) ForgetFile — verify path is symlink or ignlnk placeholder before removing. If the manifest and filesystem diverge (e.g. user replaced symlink with real file, or edited placeholder in-place), refuse and return a clear, actionable error instead of blindly removing content.

**Scope:** internal/core/fileops.go — LockFile re-lock (L73–85), UnlockFile (L183–191), ForgetFile (L214–218)
**Estimated Changes:** 1 file, 3 functions, ~30 lines

---

## Objective

### Problem / Gap / Need

LockFile (re-lock), UnlockFile, and ForgetFile assume the path matches the manifest and call `os.Remove` without verification. If the user replaced a symlink with a regular file, edited the placeholder in-place with real content, or the manifest is corrupted, the code removes real user data — causing irreversible loss.

### Success Criteria

**LockFile re-lock:**
- [ ] Re-lock when absPath is a symlink succeeds (unchanged behavior)
- [ ] Re-lock when absPath is a regular file returns an error and does NOT remove the file
- [ ] Error message clearly explains the situation and suggests remediation

**UnlockFile:**
- [ ] Unlock when path is a placeholder succeeds (unchanged behavior)
- [ ] Unlock when path is a symlink (idempotent) succeeds (unchanged)
- [ ] Unlock when path is a regular file and NOT a placeholder returns an error and does NOT remove the file
- [ ] Error message clearly explains the situation and suggests remediation

**ForgetFile:**
- [ ] Forget when path is a placeholder succeeds (unchanged behavior)
- [ ] Forget when path is a symlink succeeds (unchanged behavior)
- [ ] Forget when path is a regular file and NOT a placeholder returns an error and does NOT remove the file or vault
- [ ] Error message clearly explains the situation and suggests remediation

### Out of Scope

- Adding automated tests (separate plan: 20260212-fileops-relock-tests-plan.md)
- Divergence logging, repair tooling, `--force` flag
- Path missing (ForgetFile: create dirs and copy; current behavior preserved)

---

## Context

### Current State

**LockFile re-lock:**
```go
if entry, ok := manifest.Files[relPath]; ok && entry.State == "unlocked" {
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing symlink: %w", err)
    }
    // ... write placeholder
}
```
No verification that absPath is a symlink before removal.

**ForgetFile:**
```go
if _, err := os.Lstat(absPath); err == nil {
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing existing file: %w", err)
    }
}
```
No verification that the path is placeholder or symlink. Regular file with user data is removed.

**UnlockFile:**
```go
if info, err := os.Lstat(absPath); err == nil {
    if info.Mode().IsRegular() && !IsPlaceholder(absPath) {
        fmt.Fprintf(os.Stderr, "warning: file at %s has been modified (not a placeholder)\n", ...)
    }
    if err := os.Remove(absPath); err != nil { ... }
}
```
Warning does not prevent removal. User data is destroyed.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/fileops.go | LockFile re-lock, UnlockFile, ForgetFile | Add verification; refuse if wrong type |

### Dependencies

- **Requires:** None
- **Blocks:** 20260212-fileops-relock-tests-plan.md (tests will verify these fixes)

### Constraints

- Must preserve idempotent and re-lock semantics when path is valid symlink (LockFile)
- Must preserve unlock semantics when path is placeholder or symlink (UnlockFile)
- Must preserve forget semantics when path is placeholder or symlink (ForgetFile)
- Errors must be actionable (user can fix by unlock/lock flow or manual reconcile)

---

## Implementation

### Overview

Insert `os.Lstat` + type checks before each `os.Remove`. LockFile: path must be symlink. UnlockFile: path must be placeholder or symlink (refuse when regular file and !IsPlaceholder). ForgetFile: path must be symlink or regular file where `IsPlaceholder` returns true.

### Step 1: LockFile re-lock — symlink verification before remove

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

**Rationale:** Lstat verifies filesystem state before destructive operation. Refusing when not symlink prevents data loss. Error message guides user to unlock first, then lock.

**Verification:** Manual test: (1) unlock, replace symlink with regular file, run lock → must error; (2) unlock, leave symlink, run lock → must succeed.

---

### Step 2: ForgetFile — verify path type before remove

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

**Rationale:** Lstat + IsPlaceholder verifies filesystem state. Refusing when regular file and !IsPlaceholder prevents overwriting user edits. Error suggests locking first.

**Verification:** Manual test: (1) unlock, replace symlink with regular file with edits, run forget → must error, file and vault unchanged; (2) unlock, leave symlink, run forget → must succeed; (3) lock, leave placeholder, run forget → must succeed.

---

### Step 3: UnlockFile — refuse when path is regular file and not placeholder

**Objective:** Prevent removal of user data when user edited placeholder in-place with real content.

**Files:**
- internal/core/fileops.go

**Changes:**

```go
// Check if placeholder exists and is actually a placeholder
if info, err := os.Lstat(absPath); err == nil {
    if info.Mode().IsRegular() && !IsPlaceholder(absPath) {
        return fmt.Errorf("refusing to unlock %s: path contains user data (not a placeholder). Copy your content elsewhere, then run 'ignlnk unlock %s' again", relPath, relPath)
    }
    // Remove the placeholder (or symlink) before creating new symlink
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing placeholder: %w", err)
    }
}
```

**Rationale:** Refusing when `IsRegular && !IsPlaceholder` prevents data loss. Error tells user to back up content first, then retry unlock.

**Verification:** Manual test: (1) lock file, edit placeholder with real content, run unlock → must error, file unchanged; (2) lock file, leave placeholder, run unlock → must succeed.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

**LockFile re-lock:**
- [ ] Re-lock with symlink present → succeeds
- [ ] Re-lock with regular file at path → fails with clear error, file unchanged

**UnlockFile:**
- [ ] Unlock with placeholder at path → succeeds
- [ ] Unlock with symlink at path → succeeds (idempotent)
- [ ] Unlock with regular file (non-placeholder) at path → fails with clear error, file unchanged

**ForgetFile:**
- [ ] Forget with placeholder at path → succeeds
- [ ] Forget with symlink at path → succeeds
- [ ] Forget with regular file (non-placeholder) at path → fails with clear error, file and vault unchanged

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Symlink re-lock succeeds | Unlock file, run lock | Placeholder written, manifest updated |
| Non-symlink re-lock refused | Put regular file at path, run lock | Error returned, file not removed |
| Placeholder unlock succeeds | Lock file, run unlock | Symlink created |
| Non-placeholder unlock refused | Edit placeholder with real content, run unlock | Error returned, file not removed |
| Placeholder forget succeeds | Lock file, run forget | File restored, vault removed |
| Symlink forget succeeds | Unlock file, run forget | File restored, vault removed |
| Non-placeholder forget refused | Put real file at path, run forget | Error returned, nothing removed |

---

## Rollback Plan

If the changes cause regressions: revert the commit. The previous behavior (blind remove) is unsafe but known; rollback restores that until a corrected fix is applied.

---

## Notes

### Assumptions

- Manifest lock is held when LockFile, UnlockFile, and ForgetFile are called
- `IsPlaceholder` correctly identifies ignlnk placeholders
- User has symlink support (already checked earlier in unlock flow)
- Path missing in ForgetFile: Lstat returns err != nil; we skip remove and proceed to copy. No change to that flow.

### Risks

- Error message length: may be long; acceptable for safety-critical paths
- ForgetFile error suggests `ignlnk lock` first; if path has user edits, lock would fail (regular file, not placeholder). User may need to manually copy content to vault path, then lock, then forget. For this plan, error is sufficient.
