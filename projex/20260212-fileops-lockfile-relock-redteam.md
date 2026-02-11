# Red Team: LockFile re-lock path (fileops.go L76-L77)

> **Created:** 2026-02-12 | **Mode:** Attack | **Subject:** internal/core/fileops.go#L76-L77
> **Related:** ignlnk MVP, fileops.go

---

## Bottom Line

**Verdict:** Fix Issues

**Top Vulnerabilities:**
1. **Data loss (re-lock)** — Blind `os.Remove` trusts manifest without verifying filesystem; can delete user's real file when manifest says "unlocked"
2. **Data loss (unlock)** — UnlockFile removes absPath even when not a placeholder; user edits to placeholder are destroyed
3. **Data loss (forget)** — ForgetFile removes absPath without verifying type; real file at path overwritten by vault then vault deleted
4. **No symlink verification** — Re-lock assumes absPath is symlink based on manifest alone; stale or corrupted manifest leads to wrong removal
5. **TOCTOU** — Manifest read vs filesystem state can diverge under concurrency or external edits

---

## Stakeholder Roles

| Role | Cares About | Pain Points | Critical Assumptions |
|------|-------------|-------------|---------------------|
| End User | Data safety, predictable behavior | Losing files when re-locking | "Lock" only swaps symlink for placeholder |
| Operator | Reliability, no surprises | Unexpected failures or data loss | Filesystem matches manifest |
| Developer | Correctness, edge-case handling | Bugs from manifest/FS divergence | Manifest is source of truth |

---

## Attack Surface (L76-L77)

**Code under attack:**
```go
if entry, ok := manifest.Files[relPath]; ok && entry.State == "unlocked" {
    if err := os.Remove(absPath); err != nil {
        return fmt.Errorf("removing symlink: %w", err)
    }
    // ... writes placeholder
}
```

**Claims:** "We are removing a symlink"  
**Assumption:** absPath is a symlink because manifest says "unlocked"  
**Dependency:** Manifest ↔ filesystem consistency

---

## Critical Findings

### Finding 1: Unchecked removal — data loss when symlink was replaced
**Severity:** Critical | **Likelihood:** Medium

**Affects Roles:** End User (primary), Operator, Developer

**Attack Vector:** User unlocks file (symlink created). User copies real content from vault to project path, overwriting symlink. Manifest still says "unlocked". User runs `ignlnk lock`. Code removes whatever is at absPath (the real file), then writes placeholder.

**Blast Radius:** Permanent data loss of user's file at project path. Vault copy remains, but user may not realize until later.

**Remediation:** Verify `os.Lstat(absPath)` shows symlink before `os.Remove`. If regular file, refuse or safely handle (e.g., copy to vault first, then proceed).

---

### Finding 2: Manifest–filesystem divergence
**Severity:** High | **Likelihood:** Low–Medium

**Affects Roles:** End User, Operator

**Attack Vector:** Corrupt manifest, bad restore, or manual edit sets state="unlocked" for path that actually holds a regular file. Re-lock path triggers; real file removed.

**Blast Radius:** Data loss or unexpected behavior depending on what was at the path.

**Remediation:** Always validate filesystem state before destructive operations. Log/refuse when state mismatch detected.

---

### Finding 3: TOCTOU and external editors
**Severity:** Medium | **Likelihood:** Low

**Affects Roles:** End User, Operator

**Attack Vector:** Between manifest read and os.Remove, external process (editor, sync tool) replaces symlink with regular file. Re-lock proceeds and deletes that file.

**Blast Radius:** Data loss if external change replaced symlink with real content.

**Remediation:** Manifest lock narrows window but does not protect against external edits. Verification before remove (Finding 1) mitigates.

---

## User Data Damage: Full Attack Surface

*Extended analysis of all `fileops.go` paths that can destroy or overwrite user data.*

### Damage Vector Summary

| Path | Lines | Trigger | Blast Radius | Severity |
|------|-------|---------|--------------|----------|
| LockFile re-lock | 76-77 | Manifest says unlocked, FS has real file | **Permanent loss** of project-path file | Critical |
| UnlockFile | 189-190 | User edited placeholder with real content | **Overwrite + loss** — remove user edits, replace with vault (stale) | Critical |
| ForgetFile | 216-226 | Real file at path (user copied from vault) | **Overwrite** — remove real file, replace with vault copy | High |

### Additional Finding: UnlockFile removes non-placeholder without refusing

**Severity:** Critical | **Likelihood:** Low–Medium

**Affects Roles:** End User

**Attack Vector:** User locks file → placeholder at project path. User edits placeholder in-place (adds real content, ignores warning). Manifest still says "locked". User runs `ignlnk unlock`. Code at L184-186 warns "file has been modified" but **proceeds to `os.Remove(absPath)`** anyway. User's edits are deleted; symlink points to vault (original locked content). Edits are lost unless user has another copy.

**Blast Radius:** All in-place edits to a "placeholder" file are destroyed.

**Remediation:** If `IsPlaceholder(absPath)` is false, **refuse** unlock and return error. Suggest user copy content elsewhere, then unlock. Do not blindly remove.

---

### Additional Finding: ForgetFile removes without type check

**Severity:** High | **Likelihood:** Low

**Affects Roles:** End User

**Attack Vector:** User unlocks file (symlink). User copies real content from vault to project path, overwriting symlink. User runs `ignlnk forget`. Code removes whatever is at absPath (the real file), copies vault back, deletes vault copy. If user's local copy had edits not in vault, those are lost.

**Blast Radius:** Local modifications at project path overwritten by vault; vault then deleted. No recovery.

**Remediation:** Before remove: if absPath is regular file and content differs from vault (or is not placeholder/symlink), warn and require `--force` or refuse. Document that ForgetFile assumes path is placeholder or symlink.

---

## Remediation

### Must Fix (Before Proceeding)
- **[L76–77]** Add `os.Lstat(absPath)` before `os.Remove`. If not symlink, return error or safely handle. Prevents blind removal of user data.
- **Plan:** @20260212-fileops-verify-path-before-remove-plan.md
- **[UnlockFile L189]** If `!IsPlaceholder(absPath)` and file is regular, **refuse** unlock. Do not remove. Return error suggesting user back up content first.
- **Plan:** @20260212-fileops-verify-path-before-remove-plan.md (merged; includes UnlockFile)

### Should Fix (Before Production)
- Add tests: re-lock when absPath is symlink (OK), and when absPath is regular file (refuse or safe path).
- Document that re-lock path assumes symlink; add defensive check.
- **Plan:** @20260212-fileops-relock-tests-plan.md
- **[ForgetFile L216]** Verify absPath is placeholder or symlink before remove. If regular file with differing content, refuse or require `--force`.
- **Plan:** @20260212-fileops-verify-path-before-remove-plan.md (merged; includes ForgetFile)

### Monitor
- Log when manifest state and filesystem state diverge; consider repair/repair tooling.
- **Plan:** @20260212-fileops-divergence-logging-plan.md

---

## Final Assessment

**Soundness:** Fixable  
**Risk:** High (data loss) until verification added  
**Readiness:** Not Ready — must add symlink verification before Remove

**Conditions for Approval:**
- [ ] Lstat before Remove; refuse or safe-handle when not symlink
