# Red Team: fileops verify-path-before-remove (post-fix)

> **Created:** 2026-02-12 | **Updated:** 2026-02-12 | **Mode:** Attack | **Subject:** Post-fix verification in LockFile, UnlockFile, ForgetFile
> **Related:** 20260212-fileops-verify-path-before-remove-walkthrough.md, 20260212-fileops-lockfile-relock-redteam.md, 20260212-fileops-postfix-redteam-fixes-walkthrough.md

---

## Bottom Line

**Verdict:** Proceed (Should Fix items addressed)

**Addressed (20260212-fileops-postfix-redteam-fixes):**
- **IsPlaceholder prefix spoof** ✓ — IsPlaceholderFor adds size check; placeholder+appended content refused
- **Directory at path** ✓ — Explicit IsDir check; clear error returned
- **Pipe/socket at path** ✓ — Non-regular, non-symlink explicitly rejected with clear error

**Remaining:**
1. **TOCTOU** — Check and remove are non-atomic; external processes (editors, sync tools) that don't acquire LockManifest can still cause data loss. Documented limitation.
2. **Missing path handling** — When Lstat fails (path absent), UnlockFile proceeds to create symlink; safe but worth explicit handling for clarity (low priority)

---

## Stakeholder Roles

| Role | Cares About | Pain Points | Critical Assumptions |
|------|-------------|-------------|---------------------|
| End User | Data safety, predictable unlocks | Losing files, confusing errors | "Unlock" only swaps placeholder for symlink |
| Operator | Reliability, no surprises | Edge-case failures, TOCTOU under load | Single-user or low-concurrency use |
| Developer | Correctness, edge-case handling | Prefix-spoof, directory edge cases | IsPlaceholder is content-authoritative |

---

## Attack Surface (Post-Fix)

**Fixed code paths (current):**
- LockFile re-lock: Lstat → if !symlink refuse → Remove
- UnlockFile: Lstat → if IsDir or (!regular && !symlink) refuse → if regular && !IsPlaceholderFor(size) refuse → Remove → Symlink
- ForgetFile: Lstat → if IsDir or (!regular && !symlink) refuse → if regular && !IsPlaceholderFor(size) refuse → Remove → Copy vault back

**Addressed assumptions:**
- ~~IsPlaceholder prefix-only~~ → IsPlaceholderFor adds size check
- ~~Path could be directory, pipe, socket~~ → explicitly rejected with clear error

**Remaining assumptions:**
- Lstat and Remove see the same filesystem state (no TOCTOU from external processes)

---

## Critical Findings

### Finding 1: TOCTOU — check vs use window
**Severity:** Medium | **Likelihood:** Low

**Affects Roles:** End User, Operator

**Attack Vector:** Between Lstat/IsPlaceholder and os.Remove, an **external** process (editor, sync tool, script) — which does not acquire LockManifest — replaces the placeholder or symlink with a regular file. Remove proceeds and deletes that file. (Concurrent ignlnk operations are already serialized by LockManifest.)

**Example (UnlockFile):**
1. Lstat sees placeholder
2. IsPlaceholder returns true
3. External process overwrites file with user content
4. os.Remove deletes user data
5. Symlink created

**Blast Radius:** Data loss. Window is small but non-zero under concurrency or automated tools.

**Remediation:** We already use LockManifest (flock on `.ignlnk/manifest.lock`) — serializes concurrent ignlnk operations and mitigates ignlnk-vs-ignlnk TOCTOU. Remaining risk is external processes (editors, sync tools) that don't acquire the manifest lock. Locking absPath would require per-file flock; external tools often overwrite without respecting it. Document as known limitation.

---

### Finding 2: IsPlaceholder prefix spoof — content after prefix lost ✓ ADDRESSED
**Severity:** Medium | **Likelihood:** Low

**Status:** Fixed in 20260212-fileops-postfix-redteam-fixes. IsPlaceholderFor(path, relPath, size) now requires size == len(GeneratePlaceholder(relPath)) before accepting as placeholder. Files with appended content are refused.

**Original Attack Vector:** User creates a file that *starts* with `[ignlnk:protected]` but has extra bytes. Code would remove the file. User loses appended content.

**Remediation Applied:** Size check added; prefix+size is good enough.

---

### Finding 3: Directory at absPath — inconsistent handling ✓ ADDRESSED
**Severity:** Low | **Likelihood:** Low

**Status:** Fixed in 20260212-fileops-postfix-redteam-fixes. UnlockFile and ForgetFile now check `info.Mode().IsDir()` before Remove; return clear error "path is a directory, expected file or symlink".

**Remediation Applied:** Explicit directory check added.

---

### Finding 4: Named pipes, sockets, devices ✓ ADDRESSED
**Severity:** Low | **Likelihood:** Very Low

**Status:** Fixed in 20260212-fileops-postfix-redteam-fixes. UnlockFile and ForgetFile now check `!info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0`; return clear error "path is not a file or symlink (got ...)".

**Remediation Applied:** Non-regular, non-symlink types explicitly rejected.

---

## Role-Based Assumption Challenges

### End User: "Unlock only swaps placeholder for symlink"
**Challenge:** If their "placeholder" has been modified (prefix + extra content), it's treated as placeholder and removed. Content after prefix is lost.
**Status:** ✓ Mitigated. IsPlaceholderFor with size check refuses files with appended content.
**Action:** None — addressed.

### Operator: "Single-user or low-concurrency"
**Challenge:** Under concurrent access (CI, sync tools, multiple tabs), TOCTOU window can be hit.
**If Wrong:** Rare data loss.
**Action:** Validate — document concurrency caveats.

### Developer: "Manifest and filesystem stay in sync"
**Challenge:** Already mitigated by verification. Remaining risk: TOCTOU, prefix spoof, directory at path.
**Status:** ✓ Prefix spoof and directory addressed. TOCTOU remains documented limitation.
**Action:** Monitor — TOCTOU is low likelihood.

---

## Role-Specific Edge Cases & Failures

### End User: Edited placeholder with appended content ✓ MITIGATED
**Trigger:** User opens placeholder, adds content after the header, saves. Runs unlock.
**Status:** IsPlaceholderFor size check refuses; error returned, file unchanged.

### Operator: Path is directory after restore ✓ MITIGATED
**Trigger:** Restore or manual ops creates directory at managed path.
**Status:** Explicit IsDir check; clear error "path is a directory, expected file or symlink".

### End User: TOCTOU under automated tools
**Trigger:** Sync tool or script modifies path between check and remove.
**Experience:** Unpredictable; possible data loss.
**Recovery:** Difficult.
**Mitigation:** Document; avoid concurrent modification of same path.

---

## What's Hidden (Per Role)

**Omissions:**
- **End User:** Prefix+size check now; appended content refused.
- **Operator:** No advisory locking on absPath; concurrency vs external tools is "best effort."
- **Developer:** Directory, pipe, socket at path now explicitly rejected. ✓

**Tradeoffs:**
- Prefix+size: Defeats spoof; fast and sufficient.
- No per-file locking: Avoids complexity, but TOCTOU from external processes remains.

---

## Scale & Stress

**At 10x (more users, more paths):**
- TOCTOU likelihood increases with concurrent access. Directory/pipe/socket at path now refused. ✓

**At 100x:**
- Advisory locking on absPath may become necessary if external-tool concurrency increases.

---

## Remediation

### Must Fix (Before Proceeding)
- None — original data-loss vectors are fixed.

### Should Fix (Before Production) ✓ DONE
- ~~Directory at path~~ ✓ Addressed.
- ~~IsPlaceholder prefix spoof~~ ✓ IsPlaceholderFor with size check.

### Monitor
- TOCTOU: Add flock or equivalent if high-concurrency use emerges. Document as known limitation.

---

## Final Assessment

**Soundness:** Solid with Caveats  
**Risk:** Low  
**Readiness:** Ready

**Per-Role Readiness:**
- **End User:** Ready — prefix spoof, directory, pipe/socket addressed.
- **Operator:** Ready — TOCTOU from external processes remains documented limitation.
- **Developer:** Ready — all fixable findings addressed.

**Conditions for Approval:**
- [x] Lstat before Remove (done)
- [x] Refuse regular file when !IsPlaceholderFor (done)
- [x] Explicit directory rejection (done)
- [x] IsPlaceholder strengthening / size check (done)

**No-Go If:**
- None for current MVP scope. Proceed with awareness of TOCTOU from external processes.

---

## Situations That Can Still Break It

| Situation | Breaks? | Impact |
|-----------|---------|--------|
| User overwrites symlink with real file, then re-lock | No | Refused (not symlink) |
| User overwrites placeholder with real content, then unlock | No | Refused (!IsPlaceholderFor) |
| User copies vault to path (overwrites symlink), then forget | No | Refused (!IsPlaceholderFor) |
| External process swaps file between Lstat and Remove | Yes | TOCTOU — data loss (documented limitation) |
| File starts with `[ignlnk:protected]` but has more content | No | Refused (size check) ✓ |
| Path is directory | No | Refused with clear error ✓ |
| Path is pipe/socket | No | Refused with clear error ✓ |
| Path is symlink to regular file (re-lock) | No | Symlink removed only; target unchanged |
| Concurrent LockFile + UnlockFile on same path | No | LockManifest serializes ignlnk ops |
| Corrupt manifest says "unlocked" but path has regular file | No | Refused (re-lock checks symlink) |
