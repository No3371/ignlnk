# .gitunstage and unstage on/off Subcommands — Plan

> **Status:** Ready
> **Created:** 2026-02-12
> **Author:** agent
> **Source:** Direct request — TODO.md lines 1–2
> **Related Projex:** `20260211-ignlnk-cli-tool-proposal.md`, `20260211-ignlnk-mvp-plan.md`
> **Red Team:** `20260212-gitunstage-redteam.md` — Fix Issues (5 Must Fix before execution)

---

## Summary

Add `.gitunstage` support and `ignlnk unstage on` / `ignlnk unstage off` subcommands. `.gitunstage` uses `.gitignore`-style patterns to match files that must be unstaged before every commit. On platforms that only support `.gitignore` for untracked files, this provides a pre-commit hook–based mechanism so sensitive files never get committed while staying visible in the working tree. `unstage on` adds the hook invocation; `unstage off` removes it.

**Scope:** New `.gitunstage` semantics, three subcommands (`unstage on`, `unstage off`, internal `unstage-hook`), pre-commit hook integration
**Estimated Changes:** ~4 new files, ~2 modified files

---

## Objective

### Problem / Gap / Need

- Some environments/platforms rely on `.gitignore` but have no other way to prevent accidental commits of sensitive files.
- `.gitignore` only affects untracked files; it does not prevent committing changes to already-tracked files.
- Users want certain files to never appear in commits (invisible to agents in staged diffs, never accidentally committed), but may still need them in the working tree for builds or tooling.
- The MVP explicitly deferred git hooks; this plan fills that gap for the unstage use case.

### Success Criteria

- [ ] `.gitunstage` file at project root uses `.gitignore`-style patterns (same semantics as `.ignlnkfiles` / gitignore)
- [ ] `ignlnk unstage on` adds an invocable block to `.git/hooks/pre-commit` that calls `ignlnk unstage-hook`
- [ ] `ignlnk unstage off` removes that block from `.git/hooks/pre-commit` without damaging other hook content
- [ ] `ignlnk unstage-hook` (hidden top-level command, called by pre-commit): reads `.gitunstage`, gets staged files, unstages those matching the patterns via `git reset HEAD`
- [ ] If `.gitunstage` is missing when the hook runs, the hook no-ops (or exits 0)
- [ ] If not in a git repo, `unstage on` / `unstage off` fail with a clear error
- [ ] Idempotent: `unstage on` when already on leaves the hook unchanged; `unstage off` when already off succeeds
- [ ] `go build ./...` succeeds; `go vet ./...` passes

### Out of Scope

- Multiple `.gitunstage` files in subdirectories (single project-root file only)
- `ignlnk check` or other pre-commit validation (e.g., "unlocked files staged")
- Modifying or creating `.gitunstage` from the CLI
- Integration with the pre-commit framework (pre-commit.com)

---

## Context

### Current State

- ignlnk MVP is complete: init, lock, unlock, status, list, forget, lock-all, unlock-all.
- Git hooks were explicitly out of scope for MVP.
- `.ignlnkfiles` uses `go-gitignore` for pattern matching; logic can be reused.
- CLI uses urfave/cli v3 with a flat subcommand list.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| `cmd/app.go` | Root CLI, subcommand registration | Register `unstageCmd()` |
| `cmd/unstage.go` | New file — unstage on/off/hook logic | Create |
| `internal/core/gitunstage.go` | New file — `.gitunstage` loading, pattern matching | Create |
| `internal/core/project.go` | Project root detection | Possibly add `FindGitRoot()` or reuse `FindProject` for git root |
| `.git/hooks/pre-commit` | Git pre-commit hook | Modified by `unstage on` / `off` (user-facing) |

### Dependencies

- **Requires:** ignlnk project root detection (`core.FindProject` or equivalent) and presence of `.git/` directory
- **Requires:** `go-gitignore` (already a dependency) for `.gitunstage` pattern matching
- **Requires:** `git` in PATH when the hook runs
- **Blocks:** Nothing — additive feature

### Constraints

- Must not overwrite or replace existing pre-commit hook content; only add/remove the ignlnk block
- Use a deterministic block with markers so `unstage off` can reliably remove only our code
- Cross-platform: git commands and file paths must work on Windows, macOS, Linux

---

## Implementation

### Overview

1. Add `internal/core/gitunstage.go` — load `.gitunstage`, match paths against patterns (reuse go-gitignore).
2. Add `cmd/unstage.go` — three subcommands: `unstage on`, `unstage off`, `unstage-hook` (hidden/internal).
3. Pre-commit hook block uses clear markers for add/remove.
4. `unstage-hook` runs `git diff --cached --name-only` and unstages matching files via `git reset HEAD`.

---

### Step 1: Core — Git Root and .gitunstage Loader

**Objective:** Detect git root and load `.gitunstage` patterns.

**Files:**
- `internal/core/gitunstage.go` (new)
- `internal/core/project.go` (optional: add `GitRoot` or `HasGitDir` if not already derivable)

**Changes:**

Create `internal/core/gitunstage.go`:

```go
package core

import (
	"os"
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"
)

// FindGitRoot walks up from dir until it finds a directory containing .git
// Returns the absolute path to the repo root, or error if not in a git repo.
func FindGitRoot(dir string) (string, error)

// LoadGitunstage loads patterns from .gitunstage at project root.
// Returns nil, nil if the file does not exist (caller may no-op).
// Returns error only on read/parse failure.
func LoadGitunstage(gitRoot string) (*ignore.GitIgnore, error)

// MatchesStaged returns which of the given paths match the .gitunstage patterns.
// Paths are expected to be forward-slash normalized relative to git root.
func MatchesStaged(ignorer *ignore.GitIgnore, paths []string) []string
```

- `FindGitRoot`: start from `dir`, walk up with `filepath.Dir` until `stat(filepath.Join(p, ".git"))` succeeds (dir or file). Return that directory. Error if never found.
- `LoadGitunstage`: `path := filepath.Join(gitRoot, ".gitunstage")`. If `os.Stat` → `os.IsNotExist`, return `nil, nil`. Else `ignore.CompileIgnoreFile(path)`.
- `MatchesStaged`: for each path, `ignorer.MatchesPath(path)` — filter to those that match.

**Rationale:** Isolating git-unstage logic from vault/manifest logic. Reuses go-gitignore for consistency with `.ignlnkfiles`.

**Verification:** Unit test: temp dir with `.git` and `.gitunstage` containing `*.pem`; `LoadGitunstage` returns non-nil; `MatchesStaged` returns `config/key.pem` and not `README.md`.

---

### Step 2: Pre-commit Hook Block Markers

**Objective:** Define the exact block format for add/remove.

**Files:**
- `internal/core/hooks.go` (new) or inline in `cmd/unstage.go`

**Changes:**

Define constants for markers. Use unique suffixes (hash/UUID-like) so user content cannot collide; see Red Team Finding 1. Format: `# ignlnk-unstage-insertion-{begin|end}-{suffix}`. Document as reserved — users must not add this exact string to pre-commit.

```go
const (
	HookBlockStart = "# ignlnk-unstage-insertion-begin-a1b2c3d4\n"
	HookBlockEnd   = "# ignlnk-unstage-insertion-end-a1b2c3d4\n"
)
```

Hook body is **generated** (not constant) — depends on whether ignlnk was resolved:

1. At `unstage on`: run `exec.LookPath("ignlnk")`.
2. **If found (absolute path):** Generate body:
```
if [ -x "/abs/path/to/ignlnk" ]; then
  /abs/path/to/ignlnk unstage-hook
else
  echo "ignlnk not found at /abs/path/to/ignlnk — run 'ignlnk unstage off' then 'ignlnk unstage on' to refresh" >&2
  exit 1
fi
```
3. **If not found (PATH fallback):** Use `ignlnk` and warn; generate body:
```
if command -v ignlnk >/dev/null 2>&1; then
  ignlnk unstage-hook
else
  echo "ignlnk not found in PATH — run 'ignlnk unstage off' or add ignlnk to PATH" >&2
  exit 1
fi
```

Full block = `HookBlockStart` + generated body + `HookBlockEnd`. Use POSIX sh syntax for portability (Git for Windows runs hooks with sh).

Logic for `unstage on`:
1. Resolve git root via `FindGitRoot(cwd)`; error if not in git repo.
2. Path: `filepath.Join(gitRoot, ".git", "hooks", "pre-commit")`.
3. Resolve ignlnk via `exec.LookPath("ignlnk")`; if not found, warn and use PATH fallback.
4. Read existing content (or empty string if file missing).
5. If block already present (contains `HookBlockStart`), return nil (idempotent).
6. Generate hook body from resolved path (or fallback). **Prepend** full block to content: place our block first so unstage always runs before other hooks (see Red Team Finding 3). If content starts with shebang (`#!`), insert our block immediately after the first line (shebang + newline), then the rest.
7. Write file; set executable bit (`chmod 0755` on Unix; best-effort on Windows).
8. Print: `Unstage hook installed at .git/hooks/pre-commit` (and warn if PATH fallback used).

Logic for `unstage off`:
1. Same resolution of git root and hook path.
2. Read content. If file doesn't exist, succeed (already off).
3. Find `HookBlockStart` and `HookBlockEnd`; remove the entire block including newlines.
4. If nothing was removed and block wasn't present, succeed (idempotent).
5. If remaining content is empty or only whitespace, **remove the file** (`os.Remove`). Otherwise write back the trimmed content.
6. Print: `Unstage hook removed from .git/hooks/pre-commit`

**Rationale:** Markers allow deterministic removal without touching other hook logic. Prepend ensures unstage runs first — other hooks that exit 1 cannot prevent unstage from running. Other hooks see staging after unstage; this is usually desired (lint/format on final staged set).

**Verification:** Manual: create pre-commit with other content; run `unstage on`; verify block prepended (after shebang if present); run `unstage off`; verify block removed, other content preserved. When pre-commit contained only our block, run `unstage off`; verify file is removed.

---

### Step 3: `ignlnk unstage-hook` Implementation

**Objective:** Implement the command invoked by the pre-commit hook.

**Files:**
- `cmd/unstage.go`

**Changes:**

Subcommand `unstage-hook` (hidden: `HideHelp: true` or not user-facing in help if desired):

1. Get cwd (`os.Getwd`).
2. `FindGitRoot(cwd)` → error if not in repo.
3. `LoadGitunstage(gitRoot)` → if `nil, nil`, exit 0 (no .gitunstage).
4. `git diff --cached --name-only` → parse line-by-line for staged paths. Use `exec.Command("git", "diff", "--cached", "--name-only")` with `Dir: gitRoot`. Paths come relative to repo root; on Windows they may use backslashes.
5. **Path normalization:** Before `MatchesStaged`, normalize each path with `filepath.ToSlash(path)` — go-gitignore expects forward-slash paths. Store normalized paths for both matching and reset.
6. `MatchesStaged(ignorer, paths)` → get list to unstage (paths already normalized).
7. For each: `exec.Command("git", "reset", "HEAD", path)` with `Dir: gitRoot`. Pass normalized (forward-slash) path — Git accepts forward slashes on Windows. Run and check error.
8. If any unstaged: optionally print to stderr `Unstaged N file(s) matching .gitunstage` (or similar).
9. Exit 0.

**Rationale:** Hook must be a single command; `ignlnk unstage-hook` is self-contained and uses the same binary.

**Verification:** Create `.gitunstage` with `*.pem`, stage `a.pem` and `b.txt`, run `ignlnk unstage-hook`, verify `a.pem` unstaged and `b.txt` still staged. On Windows: verify `config\key.pem` (if git outputs backslash) is correctly unstaged when pattern is `config/*.pem`.

---

### Step 4: CLI Wiring — `unstage on` and `unstage off`

**Objective:** Expose commands to the user.

**Files:**
- `cmd/unstage.go` — full implementation
- `cmd/app.go` — register subcommand

**Changes:**

`cmd/app.go` — add:
```go
unstageCmd(),
```

`cmd/app.go` — register both the `unstage` group and the hidden hook command:
```go
unstageCmd(),
unstageHookCmd(),  // Hidden; invoked by pre-commit
```

`cmd/unstage.go` structure:
```go
// unstageCmd provides "ignlnk unstage on" and "ignlnk unstage off"
func unstageCmd() *cli.Command {
	return &cli.Command{
		Name:  "unstage",
		Usage: "Manage pre-commit hook to unstage files matching .gitunstage",
		Commands: []*cli.Command{
			{
				Name:   "on",
				Usage:  "Add unstage hook to .git/hooks/pre-commit",
				Action: runUnstageOn,
			},
			{
				Name:   "off",
				Usage:  "Remove unstage hook from .git/hooks/pre-commit",
				Action: runUnstageOff,
			},
		},
	}
}

// unstageHookCmd is invoked by the pre-commit hook; hidden from main help.
func unstageHookCmd() *cli.Command {
	return &cli.Command{
		Name:      "unstage-hook",
		Usage:     "Called by pre-commit hook; do not invoke directly",
		Hidden:    true,
		Action:    runUnstageHook,
	}
}
```

- `runUnstageOn`: implements Step 2 "unstage on" logic.
- `runUnstageOff`: implements Step 2 "unstage off" logic.
- `runUnstageHook`: implements Step 3.

**Rationale:** `unstage on`/`off` are the public API; `unstage-hook` is the internal hook target.

**Verification:** `ignlnk unstage on` then `ignlnk unstage off` in a git repo; hook file contains then lacks the block. `ignlnk unstage on` twice is idempotent.

---

### Step 5: Edge Cases and Error Handling

**Objective:** Handle missing files, empty repos, and platform differences.

**Files:**
- `cmd/unstage.go`
- `internal/core/gitunstage.go`

**Changes:**

- **No .git directory:** `FindGitRoot` returns error. `unstage on`/`off` print: `not a git repository`.
- **No .gitunstage:** `LoadGitunstage` returns `nil, nil`; `unstage-hook` exits 0 without running git.
- **Git not in PATH (hook context):** `exec.Command("git", ...)` will fail at runtime; document that git must be in PATH. No workaround in this plan.
- **ignlnk not in PATH (hook context):** `unstage on` resolves ignlnk via `LookPath` and embeds absolute path when found; otherwise uses `ignlnk` and warns. The hook tests for ignlnk before running and prints a clear error if missing.
- **ignlnk moved/reinstalled:** User runs `unstage off` then `unstage on` to refresh the embedded path. Document in AGENT.md/README.
- **Pre-commit already exists:** Prepend our block (after shebang if present); never replace. Unstage runs first so it always executes before other hooks.
- **Windows:** `git reset HEAD <path>` works. Hook file executable bit is best-effort (Windows often ignores it).
- **Path normalization (see Red Team Finding 5):** (a) Before `MatchesStaged`: normalize each path with `filepath.ToSlash(path)` — go-gitignore expects forward-slash paths per gitignore semantics. (b) For `git reset HEAD <path>`: pass the same normalized path — Git accepts forward slashes on Windows. Do not use `filepath.FromSlash` before git; forward slash is portable.

**Verification:** Run `ignlnk unstage on` outside git repo → error. Run `ignlnk unstage-hook` with no `.gitunstage` → exit 0.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

- [ ] `ignlnk unstage on` in git repo installs hook block
- [ ] `ignlnk unstage off` removes hook block cleanly; when block was only content, pre-commit file is removed
- [ ] Idempotent: `unstage on` twice, `unstage off` twice
- [ ] Create `.gitunstage` with `*.pem`, stage `key.pem` and `readme.md`, commit → only `readme.md` committed
- [ ] `ignlnk unstage-hook` with no `.gitunstage` exits 0
- [ ] `ignlnk unstage on` outside git repo fails with clear message
- [ ] Existing pre-commit content is preserved when adding/removing block
- [ ] On Windows: path normalization works (e.g. `config\key.pem` unstaged when pattern `config/*.pem`)
- [ ] Hook tests for ignlnk before running; clear error message when not found (move binary, run commit, verify message)

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| .gitunstage patterns work | Stage files, run hook | Matching files unstaged |
| `unstage on` adds block | Inspect pre-commit | Block present |
| `unstage off` removes block | Inspect pre-commit | Block absent; file removed if it was our only content |
| Idempotent | Run on/off repeatedly | No duplicate blocks, no errors |

---

## Rollback Plan

- Remove `cmd/unstage.go`.
- Remove `internal/core/gitunstage.go` (and any `internal/core/hooks.go` if created).
- Revert `cmd/app.go` registration.
- Users who ran `unstage on` can run `unstage off` to clean hooks, or manually edit `.git/hooks/pre-commit`.

---

## Notes

### Assumptions

- Git is available and in PATH when the pre-commit hook runs.
- ignlnk binary is in PATH when the hook runs.
- `.gitunstage` uses the same pattern semantics as `.gitignore` (go-gitignore).
- Single `.gitunstage` at project root is sufficient for initial scope.

### Risks

- **Git not in PATH:** Rare in dev environments; document requirement.
- **Windows hook execution:** Git Bash or similar may be needed; standard Git for Windows install configures hooks to run. Mitigation: document, test on Windows.

### Reserved Marker Format

The pre-commit hook block uses markers `# ignlnk-unstage-insertion-begin-a1b2c3d4` and `# ignlnk-unstage-insertion-end-a1b2c3d4`. Users must not add these exact strings to their pre-commit (e.g. in comments); `unstage off` would incorrectly treat them as our block boundaries. Document in AGENT.md/README.

### Open Questions

- [ ] Should `unstage-hook` print to stderr when files are unstaged, or stay silent? (Recommendation: print when something was unstaged.)
