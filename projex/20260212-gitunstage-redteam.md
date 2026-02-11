# Red Team: .gitunstage and unstage on/off — Plan

> **Created:** 2026-02-12 | **Mode:** Attack + Skeptic
> **Subject:** `20260212-gitunstage-plan.md` | **Related:** `20260211-ignlnk-cli-tool-proposal.md`, `20260211-ignlnk-mvp-plan.md`

---

## Bottom Line

**Verdict:** Fix Issues

The plan is well-structured and the design (markers, append-only, idempotency) is sound. Five issues must be addressed before execution: marker collision can corrupt user hooks, GUI/clone-path failures cause silent secret leakage, early-exit ordering hides when unstage never runs, and two spec gaps leave implementation undefined.

**Top Vulnerabilities:**
1. **Marker collision** — User content containing marker string causes `unstage off` to corrupt hook (remediation: unique markers — applied)
2. **ignlnk not in PATH when hook runs** — Mitigated: resolve at install, embed path; hook tests before running
3. **Pre-commit execution order** — Mitigated: prepend so unstage runs first; remaining: document `--no-verify` bypass
4. **Empty-pre-commit behavior** — Resolved: remove file when empty after block removal
5. **Windows path normalization** — Resolved: ToSlash before MatchesStaged; forward slash for git reset

---

## Stakeholder Roles

| Role | Cares About | Pain Points | Critical Assumptions |
|------|-------------|-------------|---------------------|
| End Users (developers) | Sensitive files never committed; simple setup | Hook doesn't run; cryptic failures | `ignlnk` in PATH; hooks run on every commit |
| Operators (CI/DevOps) | Automated commits don't break | Different PATH in CI; hook failures | Same env as local; git in PATH |
| Developers (contributors) | Clean impl, maintainable | Edge cases, platform quirks | go-gitignore behaves like .gitignore |
| Security (auditors) | No secret leakage via staged diffs | Silent bypass when hook fails | Hook always runs; users notice failures |
| Integrators (teams) | Cross-platform; shared .gitunstage | Windows vs Unix differences | Path semantics identical |
| Attackers | Commit secrets anyway | Bypass unstage | GUI, PATH, or hook order |

---

## Attack Surface (Per Role)

**End Users:**
- Claims: "files matching .gitunstage never get committed"
- Assumptions: Pre-commit runs on every commit; ignlnk is findable; patterns work like .gitignore
- Dependencies: Git in PATH; ignlnk in PATH; standard pre-commit invocation

**Operators:**
- Claims: "hook installs reliably"; "idempotent on/off"
- Assumptions: Single-threaded hook context; no env surprises in CI
- Dependencies: Standard git hook semantics; consistent PATH

**Security:**
- Claims: "sensitive files unstaged before commit"
- Assumptions: Hook runs; users see errors when it doesn't
- Dependencies: No silent bypass paths

**Integrators:**
- Claims: "works on Windows, macOS, Linux"
- Assumptions: Path format from `git diff --cached --name-only` is predictable
- Dependencies: go-gitignore MatchesPath aligns with git's path output

---

## Critical Findings

### Finding 1: Marker Collision Corrupts User Hooks

**Severity:** High | **Likelihood:** Low

**Affects Roles:** End Users, Operators

**Attack Vector:** User has existing pre-commit with a comment containing `# ignlnk unstage (begin)` (e.g., documenting another tool). `unstage off` searches for start and end markers. It finds the user's comment as "start", then our real "end" — and removes everything between, deleting user logic. Or: user manually added our block with a typo; partial block confuses removal.

**Role-Specific Impact:**
- **End Users:** Pre-commit broken; custom validation removed; may not notice until bad commit slips through
- **Operators:** CI hooks corrupted; deployment checks silently disabled

**Blast Radius:** Any pre-commit containing the literal marker string. Collision is rare but has no recovery except manual fix.

**Remediation:** Use unique markers that cannot appear by accident, e.g. `# ignlnk-unstage-insertion-begin-a1b2c3d4` (hash/UUID suffix). Document reserved marker format.

---

### Finding 2: ignlnk Not in PATH When Hook Runs

**Severity:** Critical | **Likelihood:** Medium

**Affects Roles:** End Users, Security

**Attack Vector:** User runs `ignlnk unstage on`, commits via GitHub Desktop, VS Code Source Control, or TortoiseGit. These often use different PATH than the user's terminal. `ignlnk` was installed via `go install` (in `$GOPATH/bin` or `~/go/bin`), which is in the user's login PATH but not in the GUI/git hook PATH. Hook runs `ignlnk unstage-hook` → command not found → non-zero exit → commit aborted. User sees "pre-commit hook failed" but not why. Alternatively: WSL, Docker, or sandboxed CI — ignlnk not installed or not in PATH. Result: either commit fails (bad UX) or, if hook is written to ignore errors, commit succeeds without running unstage (secret leaked).

**Role-Specific Impact:**
- **End Users:** Commit fails cryptically; or (worse) user disables/fixes hook by removing our block and commits — secrets go through
- **Security:** False sense of security; tool "installed" but hook never actually runs in common workflows

**Blast Radius:** Anyone using GUI clients, WSL, Docker-based dev, or constrained CI. Large share of Windows users commit via GUI.

**Remediation:**
- **Resolve at install time:** `unstage on` runs `exec.LookPath("ignlnk")`. If found, embed the **absolute path** in the hook. Fallback: use `ignlnk` and warn.
- **Test in the hook:** Before invoking ignlnk, the hook tests that it exists. If absolute path: `[ -x "/path/to/ignlnk" ]`. If PATH: `command -v ignlnk`. On failure, print a clear message and exit 1, e.g. "ignlnk not found at /path — run 'ignlnk unstage off' then 'ignlnk unstage on' to refresh" or "ignlnk not found in PATH — run 'ignlnk unstage off' or add ignlnk to PATH".
- **Path moves:** User can run `unstage off` then `unstage on` to refresh. Document this.

---

### Finding 3: Pre-commit Execution Order — Unstage May Never Run

**Severity:** Medium | **Likelihood:** Low (mitigated by prepend)

**Affects Roles:** End Users, Security

**Attack Vector:** ~~User has pre-commit with other hooks that run first and exit 1 — our block never runs.~~ **Mitigated:** Plan now prepends our block. Unstage runs first; other hooks cannot prevent it from running.

**Remaining Attack:** User uses `git commit --no-verify`. All hooks are bypassed. Unstage never runs. Document that unstage is bypassed with `--no-verify`.

**Role-Specific Impact:**
- **End Users:** `--no-verify` bypasses unstage; document so users understand.
- **Security:** Document `--no-verify` as known limitation; server-side hooks can backstop.

**Blast Radius:** Anyone using `--no-verify`.

**Remediation (Applied):**
- **Prepend:** Plan uses prepend — unstage block runs first. No prior hook can prevent it.
- **Document:** `git commit --no-verify` bypasses unstage. Security-conscious teams may use server-side hooks as backstop.

---

### Finding 4: Empty Pre-commit After `unstage off` — Undefined Behavior

**Severity:** Low (resolved) | **Likelihood:** N/A

**Affects Roles:** End Users, Developers

**Attack Vector:** Pre-commit contained only our block. User runs `unstage off`. Implementation choice was undefined.

**Remediation (Applied):** Remove the file when content is confirmed empty after block removal. Plan specifies: if remaining content is empty or only whitespace, `os.Remove` the pre-commit file. No empty file, no no-op stub — clean removal.

---

### Finding 5: Windows Path Normalization — Where and When

**Severity:** Low (resolved) | **Likelihood:** N/A

**Affects Roles:** Integrators, End Users

**Attack Vector:** `git diff --cached --name-only` on Windows may output paths with backslashes. go-gitignore expects forward-slash paths. Without normalization, `config\key.pem` might not match `config/*.pem`.

**Remediation (Applied):** Plan Step 3 and Step 5 now specify: (a) before `MatchesStaged`, normalize each path with `filepath.ToSlash(path)`; (b) pass the same normalized path to `git reset HEAD` — Git accepts forward slashes on Windows. No `FromSlash` needed; forward slash is portable.

---

## Role-Based Assumption Challenges

### End User: "Pre-commit runs on every commit"
**Challenge:** Git GUI clients, some IDE integrations, and `git commit --no-verify` bypass or don't run hooks. Many users commit via GUI.
**Counter-Evidence:** GitHub Desktop, TortoiseGit, VS Code (some configs) use libgit2 or custom paths.
**If Wrong:** Secrets committed despite "unstage on".
**Action:** Document; consider detecting/warning for common GUIs.

### Operator: "Same PATH in CI as local"
**Challenge:** CI runners have minimal PATH; `go install` binary not present unless explicitly added.
**If Wrong:** CI commits (e.g., automated version bumps) either fail or bypass unstage.
**Action:** Document CI requirements; add to Verification Plan.

### Security: "Users notice when hook fails"
**Challenge:** User sees "pre-commit hook failed" — not "ignlnk unstage-hook failed". May blame lint, fix lint, never realize unstage didn't run in failed attempt.
**If Wrong:** Intermittent failures hide unstage bypass.
**Action:** When unstage-hook unstages files, print to stderr (plan recommends this). When hook fails, ensure ignlnk errors are visible (not swallowed by wrapper scripts).

---

## Role-Specific Edge Cases & Failures

### End User: .gitunstage negated patterns
**Trigger:** User adds `*.pem` and `!allowed.pem`. go-gitignore supports negation. Order matters.
**Role Experience:** May not know gitignore negation; might expect `!` to work differently.
**Recovery:** Document negation and pattern order in README/AGENT.md.
**Mitigation:** Add to plan "Pattern semantics: same as .gitignore including negation; order matters."

### Operator: Pre-commit framework conflict
**Trigger:** User uses pre-commit.com framework. Our block appends to a generated file. Next `pre-commit install` may overwrite.
**Role Experience:** Our block disappears; user doesn't notice.
**Recovery:** Out of scope per plan, but document known limitation.
**Mitigation:** Note in "Out of Scope" that pre-commit framework may overwrite; users must install ignlnk block after framework or use framework's mechanism.

### Integrator: Submodule and worktree
**Trigger:** Repo has submodules; user runs hook from submodule context. FindGitRoot returns submodule root; .gitunstage at submodule root is used. Root repo's .gitunstage is ignored when committing from submodule.
**Role Experience:** Expectation unclear — per-submodule .gitunstage or inherited from root?
**Recovery:** Plan says single file at project root; "project" = git root. Submodule has its own git root. Document: each submodule has its own .gitunstage.
**Mitigation:** Explicit in plan: "git root" = root of the repo where commit runs; submodules are independent.

---

## What's Hidden (Per Role)

**Omissions per role:**
- **End Users:** GUI clients often don't run hooks or use different PATH; `--no-verify` bypasses.
- **Operators:** No CI-specific guidance; PATH and install location.
- **Security:** No server-side backstop; client-side only.
- **Developers:** go-gitignore `CompileIgnoreFile` returns error if file missing; plan correctly uses `os.Stat` first to return `nil, nil`. Good — but not called out.

**Tradeoffs per role:**
- **End Users:** Must ensure ignlnk in PATH for hook context; some workflows require extra setup.
- **Operators:** Document and test in CI before relying on it.

---

## Scale & Stress (Role Impact)

**At 10x (many staged files):**
- **End Users:** `git diff --cached --name-only` + N×`git reset` — linear in staged count. Acceptable for hundreds.
- **Operators:** Hook latency increases; may slow CI commits.

**At 100x (very large .gitunstage):**
- **Developers:** `CompileIgnoreFile` reads entire file; huge file could slow startup. Low likelihood.
- **End Users:** Pattern count doesn't affect `MatchesPath` much; go-gitignore is efficient.

---

## Remediation

### Must Fix (Before Proceeding)
- **[Finding 1] Marker collision** (affects: End Users, Operators) → Use unique markers with hash/UUID suffix → Verify no false positive on removal
- **[Finding 2] PATH in hook context** (affects: End Users, Security) → At `unstage on`, resolve via `LookPath`; embed abs path when found. Hook tests for ignlnk before running; clear error if missing → Verify hook has test + clear message
- **[Finding 4] Empty pre-commit behavior** (affects: End Users, Developers) → Remove file when content empty after removal → Verify implementation
- **[Finding 5] Path normalization** (affects: Integrators, End Users) → Applied: ToSlash before MatchesStaged; forward slash for git reset → Verify Windows test

### Should Fix (Before Production)
- **[Finding 3] --no-verify bypass** (affects: End Users, Security) → Document that `--no-verify` bypasses unstage → Add to AGENT.md/README

### Monitor
- **GUI client support** (affects: End Users) → When to revisit: if users report "hook didn't run" from specific clients
- **Pre-commit framework** (affects: Operators) → Out of scope; revisit if demand for integration

---

## Final Assessment

**Soundness:** Solid with Caveats — Core design is sound; five issues must be fixed or documented.
**Risk:** Medium — Silent bypass paths (PATH, GUI, --no-verify) can leak secrets if not documented.
**Readiness:** Needs Work — Address Must Fix items before execute.

**Per-Role Readiness:**
- **End Users:** Not Ready — PATH and GUI docs missing; marker collision possible.
- **Operators:** Not Ready — CI guidance missing.
- **Security:** Not Ready — Bypass paths not documented; users may assume coverage.
- **Integrators:** Ready with Fixes — Path normalization specified.
- **Developers:** Ready with Fixes — Implementation is clear once remediation applied.

**Conditions for Approval:**
- [ ] Unique hook markers implemented
- [ ] PATH and hook-context requirements documented
- [x] Empty pre-commit behavior specified (remove file when empty)
- [x] Path normalization explicitly in plan (Step 3, 5)
- [ ] --no-verify and execution order documented

**No-Go If:**
- [ ] Marker collision remediation not implemented (impacts all users with existing hooks)
- [ ] PATH/docs not added (impacts Windows/GUI users and Security)
