# Walkthrough: fileops post-redteam fixes (IsPlaceholder size check, directory rejection)

> **Execution Date:** 2026-02-12
> **Completed By:** agent
> **Source Plan:** 20260212-fileops-postfix-redteam-fixes-plan.md
> **Duration:** Single session
> **Result:** Success

---

## Summary

Implemented three fixes from the post-fix red team: (1) added IsPlaceholderFor with size check to defeat prefix spoof, (2) explicit directory rejection in UnlockFile and ForgetFile, and (3) rejection of pipe/socket with clear error. Added four tests. All success criteria met.

---

## Objectives Completion

| Objective | Status | Notes |
|-----------|--------|-------|
| IsPlaceholder uses size check | Complete | Added IsPlaceholderFor(path, relPath, size); callers use it |
| UnlockFile returns clear error when path is directory | Complete | IsDir() check before Remove |
| ForgetFile returns clear error when path is directory | Complete | IsDir() check before Remove |
| UnlockFile/ForgetFile reject pipe/socket with clear error | Complete | !IsRegular && !ModeSymlink check |
| All existing tests pass; new tests for size-spoof and directory | Complete | 11 tests pass, 4 new |

---

## Execution Detail

> **NOTE:** This section documents what ACTUALLY happened, derived from git history and execution notes.

### Step 1: Add IsPlaceholderFor with size check

**Planned:** Add IsPlaceholderFor(path, relPath, size) after IsPlaceholder; size must match len(GeneratePlaceholder(relPath)).

**Actual:** Added IsPlaceholderFor at fileops.go L295–303. Checks size == expected before calling IsPlaceholder(path).

**Deviation:** None.

**Files Changed (ACTUAL):**
| File | Change Type | Planned? | Details |
|------|-------------|----------|---------|
| internal/core/fileops.go | Modified | Yes | L295–303: New IsPlaceholderFor function |

**Verification:** go build ./... succeeded.

**Issues:** None.

---

### Step 2: UnlockFile — use IsPlaceholderFor and reject directory/pipe/socket

**Planned:** Add IsDir, pipe/socket check; replace IsPlaceholder with IsPlaceholderFor.

**Actual:** Added IsDir() and !IsRegular && !ModeSymlink checks before the placeholder check; replaced IsPlaceholder(absPath) with IsPlaceholderFor(absPath, relPath, info.Size()).

**Deviation:** None.

**Files Changed (ACTUAL):**
| File | Change Type | Planned? | Details |
|------|-------------|----------|---------|
| internal/core/fileops.go | Modified | Yes | L203–217: Directory, pipe/socket, IsPlaceholderFor |

**Verification:** go build, TestUnlockFile* tests passed.

**Issues:** None.

---

### Step 3: ForgetFile — use IsPlaceholderFor and reject directory/pipe/socket

**Planned:** Same pattern as UnlockFile.

**Actual:** Same checks and IsPlaceholderFor added at L238–252.

**Deviation:** None.

**Files Changed (ACTUAL):**
| File | Change Type | Planned? | Details |
|------|-------------|----------|---------|
| internal/core/fileops.go | Modified | Yes | L238–252: Directory, pipe/socket, IsPlaceholderFor |

**Verification:** go build, TestForgetFile* tests passed.

**Issues:** None.

---

### Step 4: FileStatus — use IsPlaceholderFor for consistency

**Planned:** Change IsPlaceholder(absPath) to IsPlaceholderFor(absPath, relPath, info.Size()).

**Actual:** Updated at L335.

**Deviation:** None.

**Files Changed (ACTUAL):**
| File | Change Type | Planned? | Details |
|------|-------------|----------|---------|
| internal/core/fileops.go | Modified | Yes | L335: IsPlaceholderFor |

**Verification:** go build succeeded.

**Issues:** None.

---

### Step 5: Add tests

**Planned:** Add 4 tests: size spoof refused (UnlockFile, ForgetFile), directory refused (UnlockFile, ForgetFile).

**Actual:** Added TestUnlockFilePlaceholderWithAppendedContentRefused, TestUnlockFileDirectoryRefused, TestForgetFilePlaceholderWithAppendedContentRefused, TestForgetFileDirectoryRefused.

**Deviation:** None.

**Files Changed (ACTUAL):**
| File | Change Type | Planned? | Details |
|------|-------------|----------|---------|
| internal/core/fileops_test.go | Modified | Yes | +164 lines: 4 new tests |

**Verification:** go test ./internal/core/... -v — all 11 tests pass.

**Issues:** None.

---

## Complete Change Log

> **Derived from:** `git diff --stat main..HEAD`

### Files Created
| File | Purpose | Lines | In Plan? |
|------|---------|-------|----------|
| projex/20260212-fileops-postfix-redteam-fixes-log.md | Execution log | 40 | Yes |

### Files Modified
| File | Changes | Lines Affected | In Plan? |
|------|---------|----------------|----------|
| internal/core/fileops.go | IsPlaceholderFor, UnlockFile, ForgetFile, FileStatus | +25, -4 | Yes |
| internal/core/fileops_test.go | 4 new tests | +164 | Yes |
| projex/20260212-fileops-postfix-redteam-fixes-plan.md | Status In Progress → Complete | Header | Yes |

### Files Deleted
None.

### Planned But Not Changed
None.

---

## Success Criteria Verification

| Criterion | Method | Result | Evidence |
|-----------|--------|--------|----------|
| Size check defeats prefix spoof | TestUnlockFilePlaceholderWithAppendedContentRefused | PASS | Error returned, file unchanged |
| Directory rejected in UnlockFile | TestUnlockFileDirectoryRefused | PASS | Error returned, dir unchanged |
| Directory rejected in ForgetFile | TestForgetFileDirectoryRefused | PASS | Error returned, dir unchanged |
| Placeholder+size still accepted | TestUnlockFilePlaceholderSuccess | PASS | Unlocks successfully |
| All tests pass | go test ./internal/core/... | PASS | 11/11 |

**Overall:** 5/5 criteria passed.

---

## Deviations from Plan

None.

---

## Issues Encountered

None.

---

## Key Insights

### Lessons Learned
1. **Size check is sufficient** — Prefix + size defeats spoof without hashing; keeps IsPlaceholderFor simple.

### Pattern Discoveries
1. **Path-type validation order** — Directory first, then !regular&&!symlink, then regular+IsPlaceholderFor. Same pattern in UnlockFile and ForgetFile.

### Technical Insights
- IsPlaceholder remains for internal use by IsPlaceholderFor; no external callers changed
- FileStatus now uses size-aware check for locked vs tampered; consistent with UnlockFile/ForgetFile

---

## Recommendations

### Immediate Follow-ups
- [ ] None

### Future Considerations
- 20260212-fileops-verify-path-postfix-redteam.md — Must Fix items addressed; update status
- TOCTOU remains documented limitation (no code change)

### Plan Improvements
- None; plan was accurate and complete.

---

## Related Projex Updates

### Documents to Update
| Document | Update Needed |
|----------|---------------|
| 20260212-fileops-postfix-redteam-fixes-plan.md | Add Completed, Walkthrough link |
| 20260212-fileops-verify-path-postfix-redteam.md | Should Fix items addressed |

---

## Appendix

### Test Output
```
=== RUN   TestLockFileRelockSymlink
--- PASS: TestLockFileRelockSymlink (0.02s)
=== RUN   TestLockFileRelockRegularFileRefused
--- PASS: TestLockFileRelockRegularFileRefused (0.00s)
=== RUN   TestUnlockFilePlaceholderSuccess
--- PASS: TestUnlockFilePlaceholderSuccess (0.01s)
=== RUN   TestUnlockFileRegularFileRefused
--- PASS: TestUnlockFileRegularFileRefused (0.01s)
=== RUN   TestForgetFilePlaceholderSuccess
--- PASS: TestForgetFilePlaceholderSuccess (0.01s)
=== RUN   TestForgetFileSymlinkSuccess
--- PASS: TestForgetFileSymlinkSuccess (0.01s)
=== RUN   TestForgetFileRegularFileRefused
--- PASS: TestForgetFileRegularFileRefused (0.01s)
=== RUN   TestUnlockFilePlaceholderWithAppendedContentRefused
--- PASS: TestUnlockFilePlaceholderWithAppendedContentRefused (0.01s)
=== RUN   TestUnlockFileDirectoryRefused
--- PASS: TestUnlockFileDirectoryRefused (0.00s)
=== RUN   TestForgetFilePlaceholderWithAppendedContentRefused
--- PASS: TestForgetFilePlaceholderWithAppendedContentRefused (0.01s)
=== RUN   TestForgetFileDirectoryRefused
--- PASS: TestForgetFileDirectoryRefused (0.00s)
PASS
ok      github.com/user/ignlnk/internal/core
```

### References
- Base branch: main
- Ephemeral branch: projex/20260212-fileops-postfix-redteam-fixes
- Commits: d55f78a, c846933, e0362e0, e775621, 8189554
