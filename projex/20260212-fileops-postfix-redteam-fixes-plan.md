# Plan: fileops post-redteam fixes (IsPlaceholder size check, directory rejection)

> **Status:** Ready
> **Created:** 2026-02-12
> **Author:** agent
> **Source:** Direct request — fixes from 20260212-fileops-verify-path-postfix-redteam.md
> **Related Projex:** 20260212-fileops-verify-path-postfix-redteam.md, 20260212-fileops-verify-path-before-remove-walkthrough.md

---

## Summary

Implement three fixes from the post-fix red team: (1) strengthen IsPlaceholder with a size check to defeat prefix spoof, (2) explicitly reject directory at path in UnlockFile and ForgetFile, and (3) reject other non-file types (pipe, socket) with a clear error.

**Scope:** internal/core/fileops.go and fileops_test.go only.
**Estimated Changes:** 2 files, ~50 lines added/modified.

---

## Objective

### Problem / Gap / Need

The red team identified fixable issues in the verify-path-before-remove implementation:

1. **IsPlaceholder prefix spoof** — Files that start with `[ignlnk:protected]` but have appended content pass the current check and get removed; user loses appended data.
2. **Directory at path** — UnlockFile/ForgetFile fall through to `os.Remove` when path is a directory; empty dir is removed, non-empty fails with opaque error.
3. **Pipe/socket at path** — Non-regular, non-symlink types (FIFO, socket) are not explicitly rejected.

### Success Criteria

- [ ] IsPlaceholder uses size check: file must equal `len(GeneratePlaceholder(relPath))` to be treated as placeholder
- [ ] UnlockFile returns clear error when path is directory
- [ ] ForgetFile returns clear error when path is directory
- [ ] UnlockFile and ForgetFile reject pipe/socket with clear error
- [ ] All existing tests pass; new tests cover size-spoof and directory cases

### Out of Scope

- TOCTOU mitigation (document-only in red team; no code change)
- LockFile re-lock (already handles directory via !symlink refusal)

---

## Context

### Current State

- `IsPlaceholder(path)` checks only the first 19 bytes for `[ignlnk:protected]`
- UnlockFile/ForgetFile: `if info.Mode().IsRegular() && !IsPlaceholder(absPath)` → refuse; otherwise proceed to Remove
- No explicit handling for directory, pipe, or socket

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| `internal/core/fileops.go` | Core file ops | Add IsPlaceholderFor, size check, directory/pipe rejection |
| `internal/core/fileops_test.go` | Tests | Add tests for size spoof, directory, pipe |

### Dependencies

- **Requires:** 20260212-fileops-verify-path-before-remove (already executed)
- **Blocks:** None

### Constraints

- Keep `IsPlaceholder(path)` for backward compatibility if used elsewhere; add `IsPlaceholderFor(path, relPath, size)` for size-aware check
- Use same error-message style as existing refusals

---

## Implementation

### Overview

1. Add `IsPlaceholderFor(path, relPath string, size int64) bool` — returns true only when size matches expected placeholder length and prefix matches.
2. Add helper `isExpectedPathType(info os.FileInfo) bool` or inline: path must be regular file or symlink; otherwise refuse.
3. Update UnlockFile and ForgetFile to use IsPlaceholderFor, and to refuse directory/pipe/socket before Remove.

---

### Step 1: Add IsPlaceholderFor with size check

**Objective:** Defeat prefix spoof by requiring exact placeholder size.

**Files:** `internal/core/fileops.go`

**Changes:**

Add new function after `IsPlaceholder`:

```go
// IsPlaceholderFor checks if the file at path is exactly the ignlnk placeholder for relPath.
// Requires size match (from Lstat) to defeat prefix spoof (file with appended content).
func IsPlaceholderFor(path, relPath string, size int64) bool {
	expected := int64(len(GeneratePlaceholder(relPath)))
	if size != expected {
		return false
	}
	return IsPlaceholder(path)
}
```

**Rationale:** Size is deterministic per relPath; prefix+size is sufficient. Hash would be stronger but unnecessary.

**Verification:** `go build ./...` succeeds.

---

### Step 2: UnlockFile — use IsPlaceholderFor and reject directory/pipe/socket

**Objective:** Strengthen placeholder check and add explicit path-type validation.

**Files:** `internal/core/fileops.go`

**Changes:**

Replace the block at L202–211:

```go
// Before:
if info, err := os.Lstat(absPath); err == nil {
		if info.Mode().IsRegular() && !IsPlaceholder(absPath) {
			return fmt.Errorf("refusing to unlock %s: path contains user data (not a placeholder). Copy your content elsewhere, then run 'ignlnk unlock %s' again", relPath, relPath)
		}
		// Remove the placeholder (or symlink) before creating new symlink
		if err := os.Remove(absPath); err != nil {
```

```go
// After:
if info, err := os.Lstat(absPath); err == nil {
		if info.Mode().IsDir() {
			return fmt.Errorf("refusing to unlock %s: path is a directory, expected file or symlink", relPath)
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to unlock %s: path is not a file or symlink (got %s)", relPath, info.Mode().String())
		}
		if info.Mode().IsRegular() && !IsPlaceholderFor(absPath, relPath, info.Size()) {
			return fmt.Errorf("refusing to unlock %s: path contains user data (not a placeholder). Copy your content elsewhere, then run 'ignlnk unlock %s' again", relPath, relPath)
		}
		// Remove the placeholder (or symlink) before creating new symlink
		if err := os.Remove(absPath); err != nil {
```

**Rationale:** Directory and pipe/socket are rejected first; regular file uses size-aware IsPlaceholderFor.

**Verification:** `go build ./...`; existing TestUnlockFilePlaceholderSuccess and TestUnlockFileRegularFileRefused pass.

---

### Step 3: ForgetFile — use IsPlaceholderFor and reject directory/pipe/socket

**Objective:** Same as Step 2 for ForgetFile.

**Files:** `internal/core/fileops.go`

**Changes:**

Replace the block at L233–243:

```go
// Before:
if info, err := os.Lstat(absPath); err == nil {
		if info.Mode().IsRegular() && !IsPlaceholder(absPath) {
			return fmt.Errorf("refusing to forget %s: path contains user data (not a placeholder or symlink). Run 'ignlnk lock %s' first to lock, then forget", relPath, relPath)
		}
		// Path is symlink or placeholder — safe to remove
		if err := os.Remove(absPath); err != nil {
```

```go
// After:
if info, err := os.Lstat(absPath); err == nil {
		if info.Mode().IsDir() {
			return fmt.Errorf("refusing to forget %s: path is a directory, expected file or symlink", relPath)
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to forget %s: path is not a file or symlink (got %s)", relPath, info.Mode().String())
		}
		if info.Mode().IsRegular() && !IsPlaceholderFor(absPath, relPath, info.Size()) {
			return fmt.Errorf("refusing to forget %s: path contains user data (not a placeholder or symlink). Run 'ignlnk lock %s' first to lock, then forget", relPath, relPath)
		}
		// Path is symlink or placeholder — safe to remove
		if err := os.Remove(absPath); err != nil {
```

**Rationale:** Same pattern as UnlockFile.

**Verification:** `go build ./...`; existing ForgetFile tests pass.

---

### Step 4: FileStatus — use IsPlaceholderFor for consistency

**Objective:** Ensure status "locked" vs "tampered" uses the same placeholder criteria.

**Files:** `internal/core/fileops.go`

**Changes:**

At L312, change:

```go
// Before:
if IsPlaceholder(absPath) {

// After:
if IsPlaceholderFor(absPath, relPath, info.Size()) {
```

**Rationale:** FileStatus already has info and relPath; use size-aware check for consistency.

**Verification:** `go build ./...`; status tests (if any) or manual check.

---

### Step 5: Add tests

**Objective:** Cover new behaviors: size spoof refused, directory refused.

**Files:** `internal/core/fileops_test.go`

**Changes:**

Add:

1. **TestUnlockFilePlaceholderWithAppendedContentRefused** — Create file that starts with placeholder prefix but has extra bytes (size > expected). UnlockFile must return error; file unchanged.

2. **TestUnlockFileDirectoryRefused** — Create directory at absPath. UnlockFile must return error; directory unchanged.

3. **TestForgetFilePlaceholderWithAppendedContentRefused** — Same as 1 for ForgetFile.

4. **TestForgetFileDirectoryRefused** — Same as 2 for ForgetFile.

**Rationale:** Regression protection for red team findings.

**Verification:** `go test ./internal/core/... -v` — all tests pass.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` succeeds
- [ ] `go test ./internal/core/... -v` — all tests pass (including 4 new tests)

### Manual Verification

- [ ] Run `ignlnk unlock` on a path that is a directory → clear error
- [ ] Create file = placeholder + "extra"; run `ignlnk unlock` → refused, file unchanged

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|------------------|
| Size check defeats prefix spoof | TestUnlockFilePlaceholderWithAppendedContentRefused | Error, file unchanged |
| Directory rejected in UnlockFile | TestUnlockFileDirectoryRefused | Error, dir unchanged |
| Directory rejected in ForgetFile | TestForgetFileDirectoryRefused | Error, dir unchanged |
| Placeholder+size still accepted | TestUnlockFilePlaceholderSuccess | Pass (unchanged) |

---

## Rollback Plan

If implementation causes issues:

1. Revert commits for this plan
2. Run `go test ./internal/core/...` to confirm baseline

---

## Notes

### Assumptions

- `GeneratePlaceholder(relPath)` remains deterministic and stable
- Pipe/socket at managed path is rare; clear error is sufficient

### Risks

- **info.Mode().String()** for pipe/socket may be verbose or OS-dependent; acceptable for low-frequency case

### Open Questions

- [ ] None
