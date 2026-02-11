# Red Team: ignlnk MVP Implementation Plan

> **Created:** 2026-02-11 | **Lead:** Claude (agent) | **Mode:** Attack + Forensic
> **Subject:** `20260211-ignlnk-mvp-plan.md` | **Related:** `20260211-ignlnk-cli-tool-proposal.md`
> **Reviewed:** 2026-02-11 — `20260211-ignlnk-mvp-plan-redteam-review.md`
> **Review Outcome:** Valid — all Must Fix items addressed in plan rev 2; 5 minor gaps identified

---

## Bottom Line

**Verdict:** Fix Issues

The plan is structurally sound — bottom-up build order, clear separation of concerns, correct safety invariants (atomic writes, hash verification, idempotency). But four critical implementation gaps will cause data loss, silent security failures, or platform incompatibility if shipped as specified. All four are fixable within the MVP scope.

**Top Vulnerabilities:**
1. **No concurrent access protection** — parallel invocations corrupt manifest/index via lost-update race
2. **`filepath.Match` cannot implement `.ignlnkfiles` patterns** — `**` globstar silently fails, giving false sense of security
3. **Cross-platform path separators not normalized** — manifest breaks when shared across Windows/Unix teams
4. **Windows symlinks require Developer Mode** — tool unusable for most Windows users without it, no detection or fallback

---

## Stakeholder Roles

| Role | Cares About | Pain Points | Critical Assumptions |
|------|-------------|-------------|---------------------|
| End Users (developers) | Secrets stay hidden from AI agents | Must remember to re-lock; cryptic errors on Windows | Symlinks "just work"; `.ignlnkfiles` patterns behave like `.gitignore` |
| Operators (DevOps/CI) | Automated workflows don't break | No concurrent access safety; large file hangs | CI runners have symlink support; operations complete quickly |
| Developers (contributors) | Clean, maintainable codebase | koanf complexity for simple JSON; urfave/cli v3 instability | Dependencies are stable and well-documented |
| Security (auditors) | No secret leakage; vault integrity | Unlocked state is fully exposed; no access logging | Users re-lock promptly; vault permissions are sufficient |
| Integrators (team members) | Manifest works across platforms | Path separator mismatch breaks cross-platform sharing | Manifest is portable as claimed |

---

## Attack Surface (Per Role)

**End Users:**
- Claims: "lock protects files from AI agents"
- Assumptions: `.ignlnkfiles` patterns match recursively; Windows works out of the box
- Dependencies: OS symlink support; correct glob matching; atomic file operations

**Operators:**
- Claims: "idempotent commands"; "atomic manifest writes prevent corruption"
- Assumptions: single-process execution; reasonable file sizes; fast operations
- Dependencies: no concurrent access; disk space available; no file locking interference

**Integrators:**
- Claims: "manifest is portable and committable"
- Assumptions: manifest paths work cross-platform; same patterns match same files everywhere
- Dependencies: normalized path separators; consistent glob semantics

**Security:**
- Claims: "vault UID never stored in-project"; "opaque UIDs"
- Assumptions: symlink targets not inspectable; unlocked windows are brief
- Dependencies: user discipline to re-lock; no path leakage through symlinks

---

## Critical Findings

### Finding 1: No Concurrent Access Protection

**Severity:** Critical | **Likelihood:** High

**Affects Roles:** End Users, Operators

**Attack Vector:** Two terminals run `ignlnk lock file1.txt` and `ignlnk lock file2.txt` simultaneously. Both read the manifest, both add their entry, both write. Second write overwrites first. `file1.txt` is now in the vault but absent from the manifest — orphaned and invisible.

**Role-Specific Impact:**
- **End Users:** Silent data loss. `ignlnk status` doesn't show file1. `ignlnk forget file1.txt` fails ("not managed"). Vault accumulates orphans. User may not notice until they need the file.
- **Operators:** CI jobs running `ignlnk unlock` in parallel (matrix builds, parallel test shards) corrupt manifest. Flaky, non-reproducible failures.

**Blast Radius:** Every manifest/index write is vulnerable. Includes `lock`, `unlock`, `forget`, `lock-all`, `unlock-all`, and `init` (index.json). UID collision in concurrent `init` could merge two projects' vaults — catastrophic secret cross-contamination.

**Remediation:**
- File-based exclusive lock (flock on Unix, LockFileEx on Windows) around read-modify-write cycles
- `.ignlnk/manifest.lock` for project operations; `~/.ignlnk/index.lock` for global operations
- Hold lock for entire operation (read → modify → write), not just the write
- Timeout after 30s to prevent deadlocks
- Go library: `github.com/gofrs/flock` handles cross-platform file locking

---

### Finding 2: `filepath.Match` Cannot Implement `.ignlnkfiles` Patterns

**Severity:** Critical | **Likelihood:** High

**Affects Roles:** End Users, Security

**Attack Vector:** User creates `.ignlnkfiles` with `**/*.pem` expecting all PEM files in all subdirectories to be protected. `filepath.Match` does not support `**` globstar — pattern silently matches nothing. User runs `ignlnk lock-all`, sees "locked 0 new files", and assumes no PEM files exist. Meanwhile `config/ssl/server.pem` sits unprotected, fully readable by any AI agent.

**Role-Specific Impact:**
- **End Users:** False sense of security. Believe files are protected when they're not. The failure is completely silent — no error, no warning.
- **Security:** The tool's core promise (protect files from AI agents) fails silently for the most common use case (recursive glob patterns).

**Blast Radius:** Every `.ignlnkfiles` pattern using `**`, negation (`!`), or anchoring (`/`) fails. These are the patterns users will copy directly from their `.gitignore` experience.

`filepath.Match` supports only:
- `*` — single path segment, no directory traversal
- `?` — single character
- `[a-z]` — character classes

It does NOT support: `**` (globstar), `!` (negation), `/` prefix (anchoring), or gitignore semantics.

Additional edge case: on Windows, `filepath.Match("config/*.yaml", "config\\secrets.yaml")` may fail because the separator doesn't match.

**Remediation:**
- **Option A (recommended):** Replace `filepath.Match` with `github.com/bmatcuk/doublestar/v4` — supports `**`, cross-platform separators, well-tested
- **Option B:** Use `github.com/sabhiram/go-gitignore` for full gitignore compatibility
- **Option C:** Defer `.ignlnkfiles` entirely to post-MVP — ship with explicit paths only. Better than shipping a broken feature that creates false confidence.

---

### Finding 3: Cross-Platform Path Separator Mismatch

**Severity:** Critical | **Likelihood:** High

**Affects Roles:** Integrators, End Users

**Attack Vector:** Developer on Windows runs `ignlnk lock config\secrets.yaml`. Manifest stores `"config\\secrets.yaml"`. Committed via git. Developer on macOS runs `ignlnk unlock config/secrets.yaml`. Looks up `"config/secrets.yaml"` — key not found. "File not managed" error.

**Role-Specific Impact:**
- **Integrators:** Manifest is claimed to be "portable and committable" (proposal line 131). It isn't. Cross-platform teams cannot share manifests.
- **End Users:** Commands fail with confusing "not managed" errors when they can see the file listed in manifest.json. Platform switch (e.g., moving from Windows to WSL) breaks all operations.

**Blast Radius:** Every path in `manifest.json` and every vault file path. Affects all commands that look up paths: `lock`, `unlock`, `forget`, `status`, `list`, `lock-all`, `unlock-all`.

Additional edge cases:
- **Case sensitivity:** Windows filesystem is case-insensitive. `ignlnk lock Config.yaml` then `ignlnk unlock config.yaml` — key mismatch with exact string lookup.
- **Unicode normalization:** macOS uses NFD (decomposed), Windows uses NFC (composed). Same visual filename, different byte sequences, different manifest keys.
- **Path escape:** `ignlnk lock ../outside-project/secret.txt` — `filepath.Rel` produces `../outside-project/secret.txt`, which escapes the project root. No validation prevents this.

**Remediation:**
```go
// Always normalize to forward slashes before storing
func (p *Project) RelPath(absPath string) (string, error) {
    rel, err := filepath.Rel(p.Root, absPath)
    if err != nil {
        return "", err
    }
    if strings.HasPrefix(rel, "..") {
        return "", errors.New("path outside project root")
    }
    return filepath.ToSlash(rel), nil
}

// Always convert back to OS separators when reconstructing
func (p *Project) AbsPath(relPath string) string {
    return filepath.Join(p.Root, filepath.FromSlash(relPath))
}
```

This matches git's convention (always forward slashes internally).

---

### Finding 4: Windows Symlink Privilege Requirement

**Severity:** Critical | **Likelihood:** High

**Affects Roles:** End Users, Operators

**Attack Vector:** User installs ignlnk on Windows 10. Runs `ignlnk init`, `ignlnk lock .env` — works fine (no symlinks needed). Runs `ignlnk unlock .env` — fails with `"A required privilege is not held by the client"`. User has no idea what this means or how to fix it.

**Role-Specific Impact:**
- **End Users:** Tool is half-functional on Windows out of the box. Lock works, unlock doesn't. Error message is cryptic OS jargon. Developer Mode is off by default and many users don't know it exists.
- **Operators:** Enterprise environments often disable Developer Mode via Group Policy. CI runners typically don't have it. The tool cannot be used in exactly the environments where secret protection matters most.

**Blast Radius:** Every `unlock` and `unlock-all` operation on Windows without Developer Mode. Lock and lock-all work fine (no symlinks involved).

**Remediation:**
- **Immediate:** Detect symlink capability at `ignlnk init` — attempt to create and remove a test symlink in `.ignlnk/`. Fail with clear message: "Windows Developer Mode required. Enable in Settings > Update & Security > For Developers, or run as Administrator."
- **Immediate:** Detect at `ignlnk unlock` before any file operations — fail early with actionable error.
- **Future:** Implement `--no-symlink` copy-swap fallback (currently out of scope, but its absence makes ignlnk unusable for a large Windows audience).

---

### Finding 5: Dependency Choices Add Unnecessary Risk

**Severity:** Medium | **Likelihood:** Medium

**Affects Roles:** Developers (contributors)

**Attack Vector:** koanf v2 is a multi-source configuration framework (env vars, flags, file layering, hot-reload). ignlnk reads two small fixed-schema JSON files. The entire koanf feature set is unused overhead — extra dependencies, larger binary, more attack surface, potential parsing inconsistencies (koanf's dot-delimited key access misinterprets filenames containing dots like `"config.backup.yaml"`).

urfave/cli v3 has had extended pre-release development. API churn between v3 releases has broken builds. Most community documentation and Stack Overflow answers target v2.

**Role-Specific Impact:**
- **Developers:** Debugging through koanf's abstraction layers when a simple `json.Unmarshal` would suffice. Fighting undocumented v3 behaviors.

**Remediation:**
- Replace koanf with `encoding/json` (stdlib). Zero dependencies, simpler code, more predictable.
- Consider urfave/cli v2 (battle-tested) or cobra (used by kubectl, docker, gh) instead of v3. If staying with v3, pin exact version and vendor.

---

### Finding 6: Large File Handling — No Limits, No Feedback

**Severity:** High | **Likelihood:** Medium

**Affects Roles:** End Users, Operators

**Attack Vector:** User runs `ignlnk lock database.db` on a 2GB SQLite file. SHA-256 hash + copy + verification hash = 3 full reads of 2GB each. Tool appears to hang for 30+ seconds with no output. User hits Ctrl+C — original file already deleted, vault copy incomplete. Data lost.

Alternatively: `.ignlnkfiles` matches `*.db` in a project with 50 database files totaling 20GB. `ignlnk lock-all` runs for minutes, pegs CPU at 100%, fills disk with vault copies.

**Role-Specific Impact:**
- **End Users:** Apparent hangs on large files. Ctrl+C during operation can leave inconsistent state (file gone, vault incomplete).
- **Operators:** Disk space exhaustion with no quota enforcement. CI timeouts on large file operations.

**Blast Radius:** Lock operations on files >100MB. `lock-all` with patterns matching large files.

**Remediation:**
- Progress output for operations >1 second
- Warning for files >100MB, require `--force` for >1GB
- Ensure Ctrl+C safety: don't delete original until vault copy fully verified
- The plan says "copy file to vault" then "write placeholder over original" (step 6-9 of LockFile). This order is correct — but if step 9 fails mid-rename, the original is gone and only a temp file remains. Trap signals during critical section.

---

### Finding 7: Symlink Target Path Leaks Vault Location

**Severity:** Medium | **Likelihood:** Medium

**Affects Roles:** Security

**Attack Vector:** Proposal claims "opaque UIDs" prevent agents from finding the vault. But when unlocked:

```
$ ls -la .env
.env -> C:\Users\alice\.ignlnk\vault\a1b2c3d4\.env
```

Any agent running `ls -la`, `readlink`, or inspecting file metadata sees the full vault path including UID. From there, `ls ~/.ignlnk/vault/a1b2c3d4/` reveals every file in the vault — even ones still locked.

**Role-Specific Impact:**
- **Security:** The "opaque UID" protection layer (proposal line 74-76) is defeated by a single directory listing of an unlocked file. Vault discovery is trivial when any file is unlocked.

**Blast Radius:** Every unlocked file exposes the vault path for the entire project.

**Remediation:**
- Acknowledge in documentation that symlink targets are visible. The proposal's "three layers of protection" should be revised to clarify this is only effective in the locked state.
- This is an inherent limitation of symlinks, not a bug. But it should be documented honestly rather than claimed as protection.

---

## Role-Based Assumption Challenges

### End Users: "`.ignlnkfiles` works like `.gitignore`"
**Challenge:** Plan says "patterns use filepath.Match syntax" but describes gitignore-like behavior (negation with `!`, presumably recursive matching). These are incompatible.
**Counter-Evidence:** `filepath.Match` documentation explicitly states no `**` support. No negation support.
**If Wrong:** Silent protection gaps. Files users believe are protected are readable by AI agents.
**Action:** Reject — replace filepath.Match or defer feature.

### Operators: "Atomic writes prevent corruption"
**Challenge:** Atomic writes prevent corruption from crashes, not from concurrent access. Two processes doing read-modify-write on the same file produce lost updates regardless of atomic rename.
**Counter-Evidence:** Classic TOCTOU / lost-update race. Well-documented in concurrent systems literature.
**If Wrong:** Manifest entries silently disappear. Orphaned vault files. Inconsistent state.
**Action:** Reject — implement file-based locking.

### Integrators: "Manifest is portable and committable"
**Challenge:** `filepath.Rel` returns OS-specific separators. Manifest written on Windows uses `\`, on Unix uses `/`. Same file has different keys per platform.
**Counter-Evidence:** Go documentation: "Rel returns a relative name that is lexically equivalent [using OS separators]."
**If Wrong:** Cross-platform teams cannot share manifests. Commands fail with "not managed" on different OS.
**Action:** Reject — normalize to forward slashes.

### Security: "Vault UID never appears in-project"
**Challenge:** True for locked files. False for unlocked files — symlink target contains full vault path including UID.
**Counter-Evidence:** `os.Readlink()` or `ls -la` on any unlocked file reveals `~/.ignlnk/vault/<uid>/`.
**If Wrong:** Agent can enumerate entire vault contents by reading one symlink target, then listing the vault directory.
**Action:** Validate — document honestly as locked-state-only protection.

---

## Role-Specific Edge Cases & Failures

### End Users: Placeholder Overwritten by AI Agent
**Trigger:** AI agent writes to a locked file path (e.g., generating a `.env` from template). Placeholder is replaced with agent-generated content.
**Role Experience:** User runs `ignlnk unlock .env` — integrity check detects non-placeholder content. But what happens? Plan says "warn if modified" but doesn't specify recovery. User's vault copy is still safe, but the agent-generated `.env` is lost when unlock overwrites it.
**Recovery:** Possible — vault copy intact. But agent-generated content lost.
**Mitigation:** On unlock, if non-placeholder content detected, back up the current file before replacing with symlink.

### Operators: Project Relocated Without Index Update
**Trigger:** Project directory moved (e.g., `mv ~/old-project ~/new-project`). Proposal says "running any ignlnk command from the new location auto-updates the index" — but the plan doesn't implement this auto-update.
**Role Experience:** `ignlnk status` from new location fails with "not an ignlnk project" (walks up, doesn't find `.ignlnk/`... wait, `.ignlnk/` moved with the project). Actually: `FindProject` finds `.ignlnk/` fine. But `ResolveVault` looks up the *new* project root in the index — not found. Error: "project not registered."
**Recovery:** Difficult — user must figure out they need to re-register. Existing vault files under old UID are orphaned.
**Mitigation:** If `FindProject` succeeds but `ResolveVault` fails, search index by UID from vault (but UID isn't stored in-project by design). Alternative: store a non-secret project identifier in `.ignlnk/` that can be matched against the index.

### End Users: Lock File That's Already a Symlink
**Trigger:** User has a symlink in their project (not from ignlnk) and runs `ignlnk lock link.txt`.
**Role Experience:** Plan says "verify file exists and is a regular file (not symlink, not dir)" — good, this is rejected. But error message matters. "Not a regular file" is confusing. Should say: "cannot lock symlinks — lock the target file instead."
**Recovery:** N/A — operation rejected.
**Mitigation:** Clear error message with guidance.

### Integrators: Unicode Filename Normalization
**Trigger:** macOS user creates file `café.env` (NFD: `cafe\u0301.env`). Windows user sees `café.env` (NFC: `caf\u00e9.env`). Same visual, different bytes.
**Role Experience:** Manifest key lookup fails across platforms even with forward-slash normalization.
**Recovery:** Difficult — requires Unicode normalization awareness.
**Mitigation:** Normalize all paths to NFC before storing. Go: `golang.org/x/text/unicode/norm`.

---

## What's Hidden (Per Role)

**Omissions per role:**
- **End Users:** No mention of what happens if vault disk fills up mid-lock. No mention of backup strategy for the vault. No mention of how to recover if `~/.ignlnk/` is accidentally deleted.
- **Operators:** No mention of concurrent access safety anywhere in the plan. No mention of CI/CD considerations (runners without symlink support, parallel jobs).
- **Security:** No mention that symlink targets expose vault path. No mention that conversation context retains secrets after re-lock. No access logging.
- **Integrators:** No mention of path normalization. Plan uses `filepath.Rel` without `ToSlash`, implying platform-specific paths in manifest.

**Tradeoffs per role:**
- **End Users:** Simplicity over completeness — no progress bars, no size limits, no auto-lock timer.
- **Security:** Usability over security — unlocked state is fully transparent by design, no middle ground.
- **Developers:** Speed of implementation over robustness — no file locking, no path normalization, simpler but broken.

---

## Scale & Stress (Role Impact)

**At 10x (50 managed files, 3 team members):**
- **End Users:** `lock-all` / `unlock-all` becomes slow without progress feedback. Concurrent operations from different terminals likely.
- **Operators:** Manifest merge conflicts in git become frequent. No merge strategy documented.
- **Integrators:** Path separator issues manifest immediately with mixed-OS team.

**At 100x (500 managed files, 20 team members):**
- **End Users:** Manifest.json becomes large (~50KB). Every lock/unlock rewrites entire file. Performance degrades.
- **Operators:** Vault disk usage significant. No cleanup/compaction mechanism. No way to audit vault size.
- **Security:** Attack surface grows — more files in vault, longer unlock windows, more opportunities for exposure.

---

## Remediation

### Must Fix (Before Proceeding)

1. **File-based locking for manifest and index** (affects: End Users, Operators) → Use `github.com/gofrs/flock` for cross-platform file locking around all read-modify-write cycles → Verify with concurrent lock/unlock test
2. **Path separator normalization** (affects: Integrators, End Users) → `filepath.ToSlash()` before storing, `filepath.FromSlash()` when reconstructing → Verify by writing manifest on Windows, reading on Unix (or vice versa via test)
3. **Replace `filepath.Match` or defer `.ignlnkfiles`** (affects: End Users, Security) → Use `github.com/bmatcuk/doublestar/v4` for glob matching, OR defer `.ignlnkfiles` to post-MVP → Verify with `**/*.pem` pattern matching `config/ssl/server.pem`
4. **Windows symlink detection** (affects: End Users, Operators) → Test symlink capability at `ignlnk init` and `ignlnk unlock`; fail early with actionable message → Verify on Windows without Developer Mode
5. **Path validation** (affects: Security, Integrators) → Reject `..` traversal and absolute paths in `RelPath` → Verify `ignlnk lock ../outside/file` is rejected

### Should Fix (Before Production)

6. **Replace koanf with `encoding/json`** (affects: Developers) → Remove unnecessary dependency
7. **Progress output for long operations** (affects: End Users) → Print progress for files >10MB
8. **File size warning/limit** (affects: End Users, Operators) → Warn >100MB, require `--force` >1GB
9. **Signal safety during lock** (affects: End Users) → Don't remove original until vault copy fully verified; handle Ctrl+C gracefully
10. **Unlock warnings** (affects: Security) → Print warning that file is now readable by all processes

### Monitor

11. **urfave/cli v3 stability** (affects: Developers) → Evaluate before v1.0 release; pin exact version now
12. **Vault disk usage** (affects: Operators) → Track size; add `ignlnk vault-size` in future
13. **Symlink target path visibility** (affects: Security) → Document as known limitation; revisit if copy-swap added

---

## Final Assessment

**Soundness:** Fixable — core architecture is correct; four implementation gaps need addressing
**Risk:** High (with current plan) → Low (after must-fix items)
**Readiness:** Needs Work

**Per-Role Readiness:**
- **End Users:** Not Ready — `.ignlnkfiles` silently broken, Windows unlock fails, no concurrent safety
- **Operators:** Not Ready — concurrent access corrupts state, no large file handling
- **Developers:** Ready with Fixes — dependency choices suboptimal but workable
- **Security:** Ready with Caveats — threat model is honest, but symlink path leakage and `.ignlnkfiles` false security need addressing
- **Integrators:** Not Ready — cross-platform path handling broken

**Conditions for Approval:**
- [ ] File-based locking implemented for manifest and index (for End Users, Operators)
- [ ] Path separator normalized to forward slashes (for Integrators)
- [ ] `filepath.Match` replaced with doublestar OR `.ignlnkfiles` deferred (for End Users, Security)
- [ ] Windows symlink capability detection added (for End Users, Operators)
- [ ] Path validation rejects `..` and absolute paths (for Security)

**No-Go If:**
- [ ] `.ignlnkfiles` ships with `filepath.Match` — creates false security (impacts End Users, Security)
- [ ] No concurrent access protection — guarantees data loss at scale (impacts End Users, Operators)
