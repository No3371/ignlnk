# Red Team (Pass 2): ignlnk MVP Implementation Plan (rev 2)

> **Created:** 2026-02-11 | **Lead:** Claude (agent) | **Mode:** Attack + Skeptic + Forensic
> **Subject:** `20260211-ignlnk-mvp-plan.md` (rev 2 — post first red team)
> **Related:** `20260211-ignlnk-mvp-plan-redteam.md`, `20260211-ignlnk-mvp-plan-redteam-review.md`, `20260211-ignlnk-cli-tool-proposal.md`

---

## Bottom Line

**Verdict:** Proceed with Caution

The first red team caught the structural failures (no locking, broken globbing, path separators, Windows symlinks). Rev 2 patched all four. This second pass attacks what survived: protocol inconsistencies introduced by the remediations, a critical semantic mismatch in the glob library choice, and liveness issues from the locking design. No showstoppers, but two high-severity findings need fixing before implementation.

**Top Vulnerabilities:**
1. **doublestar != gitignore** — `*.pem` in `.ignlnkfiles` silently won't match `config/ssl/server.pem` (users expect gitignore semantics; doublestar doesn't provide them)
2. **Manifest save protocol is inconsistent** — ForgetFile saves internally, LockFile/UnlockFile don't; partial failures leave orphaned files
3. **Manifest lock held for entire lock-all** — blocks all other ignlnk commands for the duration, no timeout escape

---

## Stakeholder Roles

Same roles as first red team. This pass focuses on new attack surfaces created by rev 2 changes.

| Role | New Concern in Rev 2 |
|------|---------------------|
| End Users | `.ignlnkfiles` patterns don't behave like `.gitignore` despite claiming to; lock-all hangs block status/list |
| Operators | Lock contention in CI; partial failure orphans files |
| Developers | Inconsistent save protocol will cause implementation bugs |
| Security | Vault directory permissions unspecified; symlink check tests wrong filesystem |
| Integrators | Display paths in terminal output unspecified |

---

## Attack Surface (Per Role)

**End Users:**
- Claims: "patterns use doublestar syntax (superset of filepath.Match)" and "last matching pattern wins (like .gitignore)"
- Reality: doublestar is NOT gitignore-compatible. Slash-less patterns behave differently.
- Dependencies: users' mental model of glob patterns (shaped by .gitignore experience)

**Operators:**
- Claims: "concurrent invocations don't corrupt manifest or index"
- Reality: they don't corrupt, but they DO block. Long lock-all holds the manifest lock, starving all other commands.
- Dependencies: reasonable lock hold times; error recovery on partial failure

**Developers:**
- Claims: "every command that modifies the manifest must call LockManifest"
- Reality: ForgetFile calls SaveManifest internally, violating the "caller saves" pattern used by LockFile/UnlockFile. Implementation will be confused.
- Dependencies: consistent internal API conventions

**Security:**
- Claims: vault stores sensitive files safely outside the project tree
- Reality: `~/.ignlnk/vault/` created with default umask permissions; no explicit 0700. Other users on multi-user systems can read secrets.
- Dependencies: restrictive vault permissions

---

## Critical Findings

### Finding 1: doublestar Is Not gitignore-Compatible — Silent Pattern Mismatch

**Severity:** High | **Likelihood:** High

**Affects Roles:** End Users, Security

**Attack Vector:** User writes `.ignlnkfiles`:
```
*.pem
*.env
node_modules
```

User expects gitignore behavior: `*.pem` matches `config/ssl/server.pem`, `*.env` matches `deploy/staging.env`, `node_modules` matches `packages/foo/node_modules/`.

**Actual doublestar behavior:**
- `doublestar.Match("*.pem", "config/ssl/server.pem")` → **false** (`*` doesn't cross path separators)
- `doublestar.Match("*.pem", "server.pem")` → **true** (root-level only)
- `doublestar.Match("*.env", "deploy/staging.env")` → **false**

**In gitignore:** a pattern without `/` matches against the filename component at any depth. In doublestar, `*` is a single-segment wildcard — always. The "superset of filepath.Match" claim is accurate, but it's NOT a superset of gitignore.

**Role-Specific Impact:**
- **End Users:** This is exactly the same class of bug the first red team found with filepath.Match — silent false negatives creating a false sense of security. The first red team killed `filepath.Match` for this reason. The replacement has the same bug with a different trigger.
- **Security:** Files the user believes are protected remain exposed. The `.ignlnkfiles` documentation says "like .gitignore" but the behavior differs in the most common use case.

**Blast Radius:** Every `.ignlnkfiles` pattern without an explicit `**/` prefix. This is the default way users write patterns (copied from their `.gitignore` experience).

**Why the first red team missed this:** The first red team correctly identified that filepath.Match lacks `**` support and recommended doublestar. But it didn't verify that doublestar's `*` behaves the same as gitignore's `*` in the no-slash context. The fix for one semantic gap introduced another.

**Remediation options:**
- **Option A:** Add automatic pattern rewriting in `Parse()` — if a pattern contains no `/`, prepend `**/`. This makes `*.pem` become `**/*.pem`, matching gitignore's "no-slash = match anywhere" rule. Document the rewrite clearly.
- **Option B:** Switch from doublestar to `github.com/sabhiram/go-gitignore` — purpose-built for gitignore semantics, handles slash-less patterns, negation, and anchoring natively. No manual rewriting needed.
- **Option C:** Document the difference prominently and require users to write `**/*.pem` explicitly. Least work but worst UX — users WILL get this wrong.

---

### Finding 2: Manifest Save Protocol Is Inconsistent

**Severity:** High | **Likelihood:** High (during implementation)

**Affects Roles:** Developers, End Users

**Attack Vector:** The plan specifies two conflicting patterns:

**Pattern A (LockFile, UnlockFile) — caller saves:**
- `LockFile` step 12: "Update manifest entry: state='locked', hash, lockedAt=now" — modifies in-memory manifest only
- `cmd/lock.go`: "Save manifest once at end" — caller saves after loop

**Pattern B (ForgetFile) — function saves:**
- `ForgetFile` step 6: "Save manifest" — saves internally
- `cmd/forget.go`: "Save manifest" — caller ALSO saves after loop

This creates three bugs:

**Bug 1: ForgetFile double-saves.** If user runs `ignlnk forget a.txt b.txt`, ForgetFile saves after forgetting `a.txt`, then again after `b.txt`, then cmd/forget.go saves again. Three writes instead of one. Functional but wasteful and confusing.

**Bug 2: Partial failure orphans files with Pattern A.** If `ignlnk lock a.txt b.txt c.txt` succeeds for `a.txt` and `b.txt` but fails on `c.txt`:
- `a.txt` and `b.txt` are locked on disk (vault copy + placeholder in place)
- In-memory manifest has entries for `a.txt` and `b.txt`
- `c.txt` error causes the command to... what? The plan doesn't specify.
- If the command exits without saving: `a.txt` and `b.txt` are locked on disk but NOT in the manifest. Orphaned. `ignlnk status` doesn't show them. `ignlnk forget a.txt` fails ("not managed"). User has no way to recover without manually restoring from vault.
- If the command saves partial results: `a.txt` and `b.txt` are correctly tracked, only `c.txt` failed. Better outcome but not specified.

**Bug 3: UnlockFile doesn't save — but where is the caller save for cmd/unlock.go?** The plan's Step 7 for cmd/unlock.go says "Same structure as lock" and "Call core.UnlockFile for each arg" but doesn't explicitly mention saving the manifest. Is it implied by "Same structure as lock"? An implementer might miss it.

**Role-Specific Impact:**
- **Developers:** Inconsistent internal API leads to implementation bugs. "Does this function save or not?" becomes a guessing game.
- **End Users:** Partial failure orphans files silently. Locked files with no manifest entry are invisible and unrecoverable through normal commands.

**Blast Radius:** Every multi-file command (lock, unlock, forget with multiple args; lock-all; unlock-all).

**Remediation:**
- **Standardize on caller-saves everywhere.** Remove step 6 ("Save manifest") from ForgetFile. All three operations (LockFile, UnlockFile, ForgetFile) modify in-memory manifest only. Callers in cmd/ always save once at end.
- **Specify partial failure handling:** "On error, save the manifest with all successfully completed operations before returning the error. Print per-file results (success/failure) as they happen."
- **Make cmd/unlock.go save explicit** — don't rely on "same structure as lock" implication.

---

### Finding 3: Manifest Lock Held During Entire lock-all Blocks All Other Commands

**Severity:** High | **Likelihood:** Medium

**Affects Roles:** End Users, Operators

**Attack Vector:** User runs `ignlnk lock-all` in a project with 50 files matching `.ignlnkfiles` patterns. Each file requires: hash (~200ms for 1MB) + copy (~200ms) + verify hash (~200ms) + atomic placeholder write (~10ms). Per file: ~600ms. Total: ~30 seconds.

During those 30 seconds, the manifest lock is held. In another terminal:
```
$ ignlnk status
[hangs for 30 seconds waiting for lock]
```

```
$ ignlnk unlock .env
[hangs for 30 seconds waiting for lock]
```

**Role-Specific Impact:**
- **End Users:** `ignlnk status` becomes unresponsive during lock-all/unlock-all. User thinks the tool is broken.
- **Operators:** CI jobs with lock-all + unlock in parallel time out. lock-all's lock duration is proportional to number/size of files — unbounded.

**Blast Radius:** Any command during lock-all or unlock-all execution. Status and list (read-only) are blocked unnecessarily.

**Why this wasn't in the first red team:** The first red team recommended locking but didn't analyze the liveness implications of holding a lock across a batch operation that does I/O.

**Remediation options:**
- **Option A (simple):** Read-only commands (`status`, `list`) don't acquire the manifest lock. They read the manifest file directly — atomic writes guarantee they see either the old or new version, never partial. Only mutating commands lock.
- **Option B (granular):** Release and re-acquire the lock between files in batch operations. Save manifest after each file. More I/O but no long-duration lock holds. Downside: another concurrent command could interleave.
- **Option C (timeout):** Add a lock acquisition timeout (5s default) to all commands. If lock can't be acquired, print "another ignlnk operation is in progress, try again shortly" instead of hanging indefinitely.

Recommended: **Option A + Option C.** Read-only commands skip locking; all lock acquisitions have a timeout.

---

### Finding 4: Stale Lock Cleanup Specification Is Unimplementable

**Severity:** Medium | **Likelihood:** Medium

**Affects Roles:** End Users, Developers

**Attack Vector:** The plan specifies (Step 2, line 236):
> "Stale lock detection: if `.ignlnk/manifest.lock` is >5 minutes old and the PID that created it is gone, clean it up"

This specification has three problems:

1. **gofrs/flock doesn't store PIDs.** It uses OS-level advisory locks (flock on Unix, LockFileEx on Windows). The lock file is empty or contains nothing useful. There's no PID to check.

2. **OS locks auto-release on process exit.** If a process crashes while holding a flock, the OS releases the lock automatically. The "stale lock" scenario the plan envisions (crashed process, lock still held) **cannot happen** with flock-style locks — the lock is released when the file descriptor is closed, which happens on process exit.

3. **The 5-minute timeout is both too long and too short.** Too long: user waits 5 minutes for a lock from a crashed process that already auto-released. Too short: a legitimate lock-all of 500 files could exceed 5 minutes.

**Role-Specific Impact:**
- **Developers:** Will try to implement PID-based stale detection and discover flock doesn't support it. Wasted implementation time.
- **End Users:** The scenario the plan tries to protect against (crashed process holding lock) doesn't actually occur with flock.

**Blast Radius:** LockManifest and LockIndex implementation.

**Remediation:** Remove the PID-based stale lock cleanup specification. Replace with:
- gofrs/flock's `TryLockContext` with a timeout (e.g., 30 seconds)
- On timeout, print: "Could not acquire lock. Another ignlnk operation may be running. If no other operation is active, delete `.ignlnk/manifest.lock` and retry."
- flock automatically releases on process crash — no manual cleanup needed

---

### Finding 5: CheckSymlinkSupport Tests Wrong Filesystem

**Severity:** Medium | **Likelihood:** Low

**Affects Roles:** End Users

**Attack Vector:** The plan specifies (Step 3, line 285):
> "CheckSymlinkSupport() — create and remove a test symlink in a temp dir"

The temp dir (`os.TempDir()`) may be on a different filesystem than the project:
- Project on FAT32 USB drive (no symlink support)
- Temp dir on NTFS system drive (symlinks work)
- Check passes, unlock fails later

Also: project on a network share (SMB) — symlinks may not work across network mounts even with Developer Mode.

**Role-Specific Impact:**
- **End Users:** `ignlnk init` says symlinks are supported (test passed in temp). `ignlnk unlock` fails later when trying to create symlink in the actual project directory. Confusing — "but init said it was fine!"

**Blast Radius:** Only affects users with projects on non-NTFS or network filesystems. Low likelihood but high confusion when it hits.

**Remediation:** Change CheckSymlinkSupport to create the test symlink in the `.ignlnk/` directory (which is on the same filesystem as the project). Test in the filesystem that will actually be used. Clean up the test symlink afterward.

---

### Finding 6: Vault Directory Permissions Not Specified

**Severity:** Medium | **Likelihood:** Low

**Affects Roles:** Security

**Attack Vector:** `os.MkdirAll` uses the default umask. On a multi-user Linux system with umask 022:
```
$ ls -la ~/.ignlnk/vault/
drwxr-xr-x  alice  alice  a1b2c3d4/
```

Other users can `ls` and `cat` the vault contents. The vault stores the actual sensitive files — API keys, certificates, credentials.

**Role-Specific Impact:**
- **Security:** Vault files readable by other users on shared systems. Defeats the purpose of protecting secrets.

**Blast Radius:** All vault contents on multi-user systems.

**Remediation:** Explicitly create `~/.ignlnk/` and all vault directories with `0700` permissions:
```go
os.MkdirAll(path, 0700)
```
Also set file permissions for vault files to `0600`. Check and warn if existing vault has overly permissive permissions.

---

### Finding 7: Display Path Convention Unspecified

**Severity:** Low | **Likelihood:** High

**Affects Roles:** End Users, Integrators

**Attack Vector:** On Windows, user runs:
```
> ignlnk lock config\secrets.yaml
locked: config/secrets.yaml
```

The manifest stores forward-slash paths (correct). But the terminal output also shows forward-slash paths, while the user typed backslash. This creates confusion: "I locked `config\secrets.yaml` but it says `config/secrets.yaml` — is that the same file?"

Similarly:
```
> ignlnk status
locked      config/secrets.yaml
```

User tries to copy-paste the path into another command:
```
> type config/secrets.yaml    ← forward slash from ignlnk output
```

On cmd.exe, forward slashes may not work. On PowerShell, they do. Inconsistent experience.

**Role-Specific Impact:**
- **End Users:** Confusion on Windows. Copy-paste from ignlnk output may not work in cmd.exe.
- **Integrators:** Scripts parsing ignlnk output get forward-slash paths regardless of OS.

**Blast Radius:** All terminal output showing file paths on Windows.

**Remediation:** Define a convention:
- **Option A:** Display OS-native paths in terminal (backslash on Windows, forward-slash elsewhere). Convert via `filepath.FromSlash` before printing. Matches user expectations.
- **Option B:** Always display forward-slash paths. Document this. Consistent across platforms but unfamiliar on Windows.
- **Recommended: Option A** — terminal output matches what users type. Internal storage uses forward slashes. Clear separation between display and storage.

---

### Finding 8: No Preview for lock-all

**Severity:** Low | **Likelihood:** Medium

**Affects Roles:** End Users

**Attack Vector:** User creates `.ignlnkfiles` with `**/*.yaml`. Runs `ignlnk lock-all`. Discovers it locked 47 YAML files including ones they didn't intend (test fixtures, CI configs, documentation examples). Undoing requires `ignlnk forget` on each.

**Role-Specific Impact:**
- **End Users:** No way to preview what `lock-all` will do before it does it. Broad patterns can lock unintended files.

**Remediation:** Add `--dry-run` flag to `lock-all`:
```
$ ignlnk lock-all --dry-run
Would lock 47 files:
  .env
  config/secrets.yaml
  deploy/staging.env
  ... (44 more)
```

Minimal implementation: run DiscoverFiles, print results, skip LockFile calls.

---

### Finding 9: Vault Location Ignores XDG Convention

**Severity:** Low | **Likelihood:** Low

**Affects Roles:** Operators

**Attack Vector:** On Linux, `~/.ignlnk/` violates the XDG Base Directory Specification. Tools like chezmoi, pass, and docker-credential-helpers use `$XDG_DATA_HOME` (~/.local/share/). Users with constrained home directories or NFS-mounted homes may not expect large vault data in `~/`.

**Role-Specific Impact:**
- **Operators:** Home directory quotas exceeded. Backup tools include vault inadvertently. Dot-directory bloat in `~/`.

**Blast Radius:** Linux/macOS users with non-standard home directory configurations. Very few affected.

**Remediation:** For MVP, document `~/.ignlnk/` as the default location. Future: respect `$ignlnk_HOME` environment variable for override. XDG compliance can come post-MVP.

---

## Role-Based Assumption Challenges

### End Users: ".ignlnkfiles works like .gitignore"

**Challenge:** The plan says "like .gitignore" (Step 5, line 409) but uses doublestar, which doesn't implement gitignore's "slash-less pattern matches at any depth" rule. This is the exact same class of false-security bug the first red team found.
**Counter-Evidence:** `doublestar.Match("*.pem", "config/ssl/server.pem")` returns false. Verified via doublestar documentation and GitHub issues. `*` only matches within a single path segment.
**If Wrong:** Same impact as the original filepath.Match finding: silent false negatives, unprotected files, false sense of security.
**Action:** Reject the "like .gitignore" claim — either implement gitignore semantics (rewrite or library swap) or remove the comparison from documentation.

### Operators: "File-based locking prevents concurrent corruption"

**Challenge:** True for correctness. But the locking design creates a liveness problem: lock-all holds the manifest lock for the entire batch operation, blocking all other commands including read-only ones.
**Counter-Evidence:** Lock-all with 50 files takes ~30 seconds of lock hold time. During this window, `ignlnk status` hangs.
**If Wrong:** User thinks tool is broken. CI jobs time out.
**Action:** Validate locking for correctness but fix liveness — exempt read-only commands from locking.

### Developers: "Consistent internal API patterns"

**Challenge:** ForgetFile saves manifest internally. LockFile and UnlockFile don't. An implementer will either follow one pattern and break the other, or discover the inconsistency mid-implementation and have to refactor.
**Counter-Evidence:** Step 4 ForgetFile step 6 says "Save manifest." Step 4 LockFile step 12 says "Update manifest entry" (no save). Step 9 cmd/forget.go says "Save manifest" (double save).
**If Wrong:** Bugs in first implementation. Extra review cycles.
**Action:** Reject — standardize on caller-saves for all three operations.

---

## Role-Specific Edge Cases & Failures

### End Users: lock-all + Ctrl+C = partial state

**Trigger:** `ignlnk lock-all` is locking 20 files. User hits Ctrl+C after file 12 is locked.
**Role Experience:** Signal safety (from first red team) protects individual files — no single file is half-locked. But the manifest hasn't been saved yet (caller saves at end). Files 1-12 are locked on disk but not in the manifest. User runs `ignlnk status` — shows no files. Runs `ignlnk lock-all` again — tries to lock files 1-12 again but they're now placeholders (not regular files), so LockFile step 3 rejects them ("not a regular file"). Stuck state.
**Recovery:** Manual: delete placeholders, copy files from vault back. No ignlnk command can fix this automatically.
**Mitigation:** Save manifest after each file in lock-all (not just at end), so Ctrl+C leaves a consistent partial state. Or: register a signal handler that saves the manifest before exiting.

### Operators: Concurrent lock-all in CI matrix

**Trigger:** CI matrix with 4 shards, each running `ignlnk lock-all` before tests.
**Role Experience:** First shard acquires lock. Shards 2-4 block on lock acquisition. With a 30-second timeout, they all fail. With no timeout, they all hang until shard 1 finishes.
**Recovery:** Sequential execution (remove parallelism) or pre-lock before matrix.
**Mitigation:** lock-all timeout message should suggest serializing operations.

### End Users: Large file unlock hashes entire vault file

**Trigger:** User locked a 500MB file. Now unlocks it. UnlockFile step 5: "Verify vault file hash matches manifest."
**Role Experience:** `ignlnk unlock large.db` takes 2-5 seconds for the hash verification. No progress output (size gate from first red team only applies to LockFile, not UnlockFile). User thinks it's hanging.
**Recovery:** N/A — just slow.
**Mitigation:** Apply the same >10MB progress output to hash verification in UnlockFile, not just LockFile.

---

## What's Hidden (Per Role)

**Omissions per role:**
- **End Users:** No mention that doublestar `*` behaves differently from gitignore `*`. The `.ignlnkfiles` documentation will mislead users who copy patterns from `.gitignore`.
- **Operators:** No mention of lock contention during batch operations. No lock acquisition timeout specified.
- **Developers:** No consistent save protocol. ForgetFile saves, LockFile/UnlockFile don't. Plan doesn't state this explicitly — developer has to infer from reading step-by-step logic.
- **Security:** Vault directory permissions not specified. Default umask on multi-user Linux = world-readable vault.

**Tradeoffs per role:**
- **End Users:** Simplicity (one library) over correctness (gitignore fidelity).
- **Operators:** Correctness (manifest locking) over liveness (commands blocked during lock-all).
- **Developers:** Brevity (ForgetFile is self-contained) over consistency (mixed save patterns).

---

## Remediation

### Must Fix (Before Implementation)

1. **Fix doublestar/gitignore semantic mismatch** (affects: End Users, Security) → Either auto-rewrite slash-less patterns to prepend `**/` in Parse(), or switch to go-gitignore library → Verify `*.pem` matches `config/ssl/server.pem`
2. **Standardize manifest save protocol** (affects: Developers, End Users) → Remove SaveManifest from ForgetFile internals. All operations modify in-memory only. Callers in cmd/ save. Specify partial failure handling: save successfully completed operations before returning error.
3. **Add lock acquisition timeout** (affects: End Users, Operators) → Use `flock.TryLockContext` with 30-second timeout. Print actionable message on timeout.

### Should Fix (Before Production)

4. **Exempt read-only commands from locking** (affects: End Users) → `status` and `list` read manifest directly without acquiring lock. Atomic writes guarantee consistent reads.
5. **Remove stale lock cleanup specification** (affects: Developers) → flock auto-releases on process exit. PID-based detection is unnecessary and unimplementable with gofrs/flock. Replace with timeout + manual cleanup instructions.
6. **Test symlinks in project filesystem** (affects: End Users) → CheckSymlinkSupport should create test symlink in `.ignlnk/`, not `os.TempDir()`.
7. **Set vault permissions to 0700/0600** (affects: Security) → `os.MkdirAll(vaultDir, 0700)` and vault file writes with `0600`.
8. **Define display path convention** (affects: End Users) → Display OS-native paths in terminal output via `filepath.FromSlash`.
9. **Apply progress output to unlock hash verification** (affects: End Users) → Same >10MB progress rule as lock.

### Consider for MVP

10. **Add `--dry-run` to lock-all** (affects: End Users) → Preview what would be locked without doing it.
11. **Save manifest after each file in lock-all** (affects: End Users) → Ctrl+C leaves consistent partial state instead of orphaning all locked files.
12. **Document vault location and override** (affects: Operators) → `~/.ignlnk/` is default. Document `$ignlnk_HOME` for future override.

---

## Final Assessment

**Soundness:** Solid with Caveats — architecture correct, four first-round fixes applied, but remediation introduced new gaps
**Risk:** Medium (with current plan) → Low (after must-fix items)
**Readiness:** Ready with Fixes

**Per-Role Readiness:**
- **End Users:** Not Ready — doublestar semantics will mislead .gitignore users; lock-all blocks status
- **Operators:** Ready with Fixes — lock timeout needed; partial failure handling needed
- **Developers:** Not Ready — inconsistent save protocol will cause implementation bugs
- **Security:** Ready with Caveats — vault permissions need specification; symlink check filesystem needs correction
- **Integrators:** Ready with Fixes — display path convention needed

**Conditions for Approval:**
- [ ] doublestar/gitignore semantic gap resolved (auto-rewrite or library swap)
- [ ] Save protocol standardized to caller-saves for all operations
- [ ] Lock acquisition timeout added (30s)
- [ ] Partial failure specified: save successful ops, report per-file results

**No-Go If:**
- [ ] `.ignlnkfiles` ships with doublestar raw semantics and "like .gitignore" documentation — same class of silent false-security as the original filepath.Match bug
