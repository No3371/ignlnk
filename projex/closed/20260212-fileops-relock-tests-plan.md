# Plan: LockFile re-lock — tests and documentation

> **Status:** Complete
> **Created:** 2026-02-12
> **Completed:** 2026-02-12
> **Walkthrough:** 20260212-fileops-relock-tests-walkthrough.md
> **Source:** @20260212-fileops-lockfile-relock-redteam.md (Should Fix)
> **Related Projex:** 20260212-fileops-lockfile-relock-redteam.md, 20260212-fileops-relock-symlink-verification-plan.md

---

## Summary

Add Go tests for the LockFile re-lock path: one verifying correct behavior when absPath is a symlink, one verifying refusal (no data loss) when absPath is a regular file. Document the defensive check in code.

**Scope:** internal/core/fileops_test.go (new file)
**Estimated Changes:** 1 new file, ~80–120 lines

---

## Objective

### Problem / Gap / Need

The red team identified that re-lock behavior must be validated by tests. Currently there are no Go tests for fileops. Tests will prevent regressions and document expected behavior for the symlink-vs-regular-file cases.

### Success Criteria

- [ ] Test: re-lock when absPath is symlink → succeeds
- [ ] Test: re-lock when absPath is regular file → returns error, file unchanged
- [ ] Tests run with `go test ./internal/core/...` and pass
- [ ] Code comment documents that re-lock path validates symlink before remove

### Out of Scope

- Full LockFile test coverage (only re-lock path)
- Integration/E2E tests (manual procedure exists)
- Other fileops functions

---

## Context

### Current State

- No `internal/core/*_test.go` files exist
- Manual test procedure in tests/manual-test-procedure.md
- LockFile re-lock fix will be in place (Plan 1) before or with this plan

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| internal/core/fileops_test.go | New file | LockFile re-lock tests |

### Dependencies

- **Requires:** 20260212-fileops-relock-symlink-verification-plan.md (tests verify that fix)
- **Blocks:** None

### Constraints

- Tests must work on Windows (symlink support required; skip if unavailable)
- Use temp dirs, clean up after tests
- Avoid modifying global state (manifest.lock, etc.)

---

## Implementation

### Overview

Create `internal/core/fileops_test.go` with helper to set up a minimal project/vault/manifest, and two test functions: one for symlink re-lock success, one for regular-file refusal.

### Step 1: Create fileops_test.go and test helpers

**Objective:** Add test file with setup helpers for LockFile tests.

**Files:**
- internal/core/fileops_test.go

**Changes:**

```go
package core

import (
    "os"
    "path/filepath"
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
    p := &Project{Root: tmp, IgnlnkDir: ignlnkDir}
    v := &Vault{Dir: filepath.Join(ignlnkDir, "vault")}
    m := &Manifest{Version: 1, Files: make(map[string]*FileEntry)}
    return p, v, m, func() {}
}
```

**Rationale:** t.TempDir() auto-cleans; minimal setup for unit tests.

**Verification:** `go test -c ./internal/core` builds.

---

### Step 2: Test re-lock when path is symlink (success)

**Objective:** Verify re-lock succeeds when absPath is a symlink.

**Files:**
- internal/core/fileops_test.go

**Changes:**

Add test that:
1. Creates relPath as symlink to a vault file
2. Sets manifest entry state="unlocked"
3. Calls LockFile
4. Asserts no error, entry.State=="locked", absPath is placeholder

**Rationale:** Documents and enforces correct happy path.

**Verification:** `go test ./internal/core/... -run TestLockFileRelockSymlink -v` passes.

---

### Step 3: Test re-lock when path is regular file (refuse)

**Objective:** Verify re-lock returns error and does not remove file when absPath is regular file.

**Files:**
- internal/core/fileops_test.go

**Changes:**

Add test that:
1. Creates relPath as regular file with known content
2. Sets manifest entry state="unlocked"
3. Calls LockFile
4. Asserts error != nil
5. Asserts file at absPath still exists and content unchanged

**Rationale:** Prevents regression of the data-loss bug.

**Verification:** `go test ./internal/core/... -run TestLockFileRelockRegularFileRefused -v` passes.

---

### Step 4: Document defensive check in fileops.go

**Objective:** Add comment documenting re-lock path's symlink validation.

**Files:**
- internal/core/fileops.go

**Changes:**

Ensure the re-lock block has a clear comment (may already exist from Plan 1):

```go
// Re-locking: if file is already managed and unlocked (symlink), swap symlink for placeholder.
// We verify absPath is a symlink before removing — if it's a regular file, refuse to avoid data loss.
```

**Rationale:** Future maintainers understand why the check exists.

**Verification:** Code review confirms comment present.

---

## Verification Plan

### Automated Checks

- [ ] `go test ./internal/core/...` passes
- [ ] `go vet ./...` reports no issues
- [ ] Tests pass on Windows (Developer Mode) and Unix

### Manual Verification

- [ ] Both new tests appear in `go test -v` output and pass

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Symlink re-lock test | go test -run TestLockFileRelockSymlink | PASS |
| Regular file refused test | go test -run TestLockFileRelockRegularFileRefused | PASS |
| Comment present | Read fileops.go re-lock block | Comment documents symlink check |

---

## Rollback Plan

Delete internal/core/fileops_test.go and revert any comment changes. No runtime behavior change.

---

## Notes

### Assumptions

- Symlink support available in test environment (or tests skip on Windows without Developer Mode)
- LockFile is called without manifest lock in tests; single-threaded tests only

### Risks

- Windows without Developer Mode: tests may need build constraint or skip

### Open Questions

- [ ] Should tests use CheckSymlinkSupport / ensureSymlinkSupport and skip if unsupported? (Recommend: yes, skip with t.Skip)
