# Plan: UnlockFile — refuse when path is not placeholder

> **Status:** Merged
> **Merged into:** 20260212-fileops-verify-path-before-remove-plan.md
> **Created:** 2026-02-12
> **Source:** @20260212-fileops-lockfile-relock-redteam.md (Must Fix — UnlockFile)
> **Related Projex:** 20260212-fileops-lockfile-relock-redteam.md

---

## Summary

Add a check in UnlockFile so that when a regular file exists at the project path, we **refuse** unlock if the file is not an ignlnk placeholder. Currently the code warns but proceeds to `os.Remove`, destroying user edits. This change prevents data loss when users edit the placeholder in-place with real content.

**Scope:** internal/core/fileops.go UnlockFile (L183–191)
**Estimated Changes:** 1 file, 1 function, ~10 lines

---

## Objective

### Problem / Gap / Need

UnlockFile creates a symlink by removing whatever is at the project path and replacing it. When the path holds a regular file that is **not** a placeholder (user edited it in-place with real content), the code prints a warning but still calls `os.Remove(absPath)`. User edits are lost; the symlink points to vault content only.

### Success Criteria

- [ ] Unlock when path is a placeholder succeeds (unchanged behavior)
- [ ] Unlock when path is a symlink (idempotent, already unlocked) succeeds (unchanged)
- [ ] Unlock when path is a regular file and NOT a placeholder returns an error and does NOT remove the file
- [ ] Error message clearly explains the situation and suggests remediation

### Out of Scope

- Adding automated tests (separate plan)
- Symlink path handling (path missing = create symlink; symlink = no-op idempotent)
- `--force` override to remove non-placeholder (future consideration)

---

## Context

### Current State

```go
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
```

The warning does not prevent removal. User data is destroyed.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/fileops.go | UnlockFile | Add refuse when regular file and !IsPlaceholder |

### Dependencies

- **Requires:** None
- **Blocks:** None (tests can be added separately)

### Constraints

- Must preserve idempotent unlock when path is missing (create symlink)
- Must preserve unlock when path is symlink (already unlocked = no-op earlier in flow)
- Error must be actionable (user can copy content elsewhere, then unlock)

---

## Implementation

### Overview

Before `os.Remove`, check: if path is a regular file and `!IsPlaceholder(absPath)`, return an error and refuse. Otherwise proceed as today. Symlinks and placeholders are removed; missing path skips remove and creates symlink.

### Step 1: Refuse unlock when path is regular file and not placeholder

**Objective:** Prevent removal of user data when user edited placeholder in-place.

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

- [ ] Unlock with placeholder present → succeeds
- [ ] Unlock with regular file (non-placeholder) at path → fails with clear error, file unchanged

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Placeholder unlock succeeds | Lock file, run unlock | Symlink created |
| Non-placeholder refused | Put real content at path, run unlock | Error returned, file not removed |

---

## Rollback Plan

If the change causes regressions: revert the commit. The previous behavior (remove anyway) is unsafe but known; rollback restores that until a corrected fix is applied.

---

## Notes

### Assumptions

- Manifest lock is held when UnlockFile is called
- `IsPlaceholder` correctly identifies ignlnk placeholders (prefix check)

### Risks

- Error message length: may be long; acceptable for safety-critical path
- Edge case: directory at path — Lstat would show IsDir, not IsRegular; we'd fall through to Remove. Directories are not placeholders; `os.Remove` on a directory fails unless empty. Out of scope for this plan; could be separate hardening.
