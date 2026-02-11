# Plan: Manifest–filesystem divergence logging

> **Status:** Ready
> **Created:** 2026-02-12
> **Source:** @20260212-fileops-lockfile-relock-redteam.md (Monitor)
> **Related Projex:** 20260212-fileops-lockfile-relock-redteam.md, 20260212-fileops-verify-path-before-remove-plan.md

---

## Summary

When LockFile detects manifest–filesystem divergence (manifest says "unlocked" but path is not a symlink), log a diagnostic message to stderr before returning the error. Satisfies the Monitor remediation for observability.

**Scope:** internal/core/fileops.go (re-lock refusal path)
**Estimated Changes:** 1 file, ~2 lines

---

## Objective

### Problem / Gap / Need

The red team Monitor remediation: "Log when manifest state and filesystem state diverge." When we refuse re-lock because absPath is not a symlink, we have detected divergence. Logging helps operators debug and surfaces the condition for monitoring.

### Success Criteria

- [ ] When LockFile refuses re-lock (path not symlink), a log line is written to stderr
- [ ] Log includes relPath for correlation
- [ ] No change to returned error or control flow

### Out of Scope

- Structured logging, log levels, or configurable logging
- Divergence logging in other code paths (UnlockFile, FileStatus)
- Repair tooling

---

## Context

### Current State

Plan 1 adds the Lstat check and returns an error when path is not symlink. No logging on that path yet.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/fileops.go | LockFile re-lock branch | Add fmt.Fprintf(os.Stderr, ...) before return |

### Dependencies

- **Requires:** 20260212-fileops-verify-path-before-remove-plan.md (logging is in the refusal path that plan adds)
- **Blocks:** None

### Constraints

- Use stderr for consistency with existing warnings (e.g., L102, L176, L185)
- Keep message concise and actionable

---

## Implementation

### Overview

In the re-lock branch, when `info.Mode()&os.ModeSymlink == 0`, add a `fmt.Fprintf(os.Stderr, "warning: ...")` before returning the error. Merge with Plan 1 implementation or apply as follow-up.

### Step 1: Add divergence log before refusal

**Objective:** Log when we refuse re-lock due to non-symlink.

**Files:**
- internal/core/fileops.go

**Changes:**

```go
if info.Mode()&os.ModeSymlink == 0 {
    fmt.Fprintf(os.Stderr, "warning: manifest says unlocked but %s is not a symlink (manifest/filesystem divergence)\n", filepath.FromSlash(relPath))
    return fmt.Errorf("refusing to re-lock %s: path is not a symlink (may contain user data). Run 'ignlnk unlock %s' first, then lock again", relPath, relPath)
}
```

**Rationale:** Matches existing warning style; provides diagnostic without changing behavior.

**Verification:** Run lock on path with regular file when manifest says unlocked → stderr shows warning, error returned.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

- [ ] Trigger refusal case, capture stderr → contains "manifest says unlocked" and relPath

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Log on refusal | Trigger re-lock refusal, inspect stderr | Warning line present with relPath |

---

## Rollback Plan

Remove the fmt.Fprintf line. No behavioral change.

---

## Notes

### Assumptions

- Plan 1 is implemented (refusal path exists)
- Can be implemented as part of Plan 1 in a single edit

### Implementation Note

This plan can be executed together with 20260212-fileops-verify-path-before-remove-plan.md as a single change: add the check, the log, and the error return in one edit.
