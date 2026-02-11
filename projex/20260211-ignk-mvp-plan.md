# ignk MVP Implementation

> **Status:** Draft (rev 3 — post red team pass 2)
> **Created:** 2026-02-11
> **Author:** Claude (agent) + user
> **Source:** `20260211-ignk-cli-tool-proposal.md`
> **Related Projex:** `20260211-ignk-cli-tool-proposal.md`, `20260211-ignk-mvp-plan-redteam.md`, `20260211-ignk-mvp-plan-redteam-review.md`, `20260211-ignk-mvp-plan-redteam-2.md`

---

## Summary

Implement the MVP of `ignk` — a Go CLI tool that protects sensitive files from AI agents using placeholder files and symlinks. Covers project initialization, lock/unlock operations, file listing/status, forget, bulk operations, and `.ignkfiles` declarative patterns.

**Scope:** All core commands (init, lock, unlock, status, list, forget, lock-all, unlock-all) + `.ignkfiles` support
**Estimated Changes:** ~15 new files

---

## Objective

### Problem / Gap / Need

No implementation exists yet. The accepted proposal (`20260211-ignk-cli-tool-proposal.md`) defines the design — this plan turns it into working code.

### Success Criteria

- [ ] `ignk init` creates `.ignk/` in project root, registers project in `~/.ignk/index.json`, creates vault directory
- [ ] `ignk lock <path>...` moves files to vault, writes placeholders, updates manifest
- [ ] `ignk unlock <path>...` replaces placeholders with symlinks to vault copies
- [ ] `ignk status` shows managed files and their current state (locked/unlocked/dirty/missing)
- [ ] `ignk list` lists all managed files
- [ ] `ignk forget <path>...` restores files from vault, removes from management
- [ ] `ignk lock-all` locks all managed + `.ignkfiles`-matched files
- [ ] `ignk unlock-all` unlocks all managed files
- [ ] `.ignkfiles` patterns are parsed and used by `lock-all` to discover new files
- [ ] Placeholder content includes the actual file path
- [ ] SHA-256 hash verification on lock and unlock
- [ ] Idempotent commands (lock on locked = no-op, unlock on unlocked = no-op)
- [ ] Cross-platform: works on Windows, macOS, Linux
- [ ] Windows: symlink capability detected at init and unlock with actionable error
- [ ] Concurrent invocations don't corrupt manifest or index (file-based locking)
- [ ] Large files (>100MB) produce a warning; files >1GB require `--force`
- [ ] Original file never deleted until vault copy fully verified (signal safety)
- [ ] Partial failures save successfully completed operations to manifest before returning error
- [ ] `lock-all --dry-run` previews files that would be locked without locking them
- [ ] Terminal output displays OS-native paths (backslash on Windows)
- [ ] `go build` produces a working binary

### Out of Scope

- Git pre-commit hooks (`ignk check` / `ignk hook install`)
- `ignk relocate` command
- goreleaser / release automation (just needs to `go build`)
- Shell completions
- Copy-swap fallback (`--no-symlink`)

---

## Context

### Current State

Empty repository. Greenfield Go project.

### Key Files

| File | Purpose | Changes Needed |
|------|---------|----------------|
| `go.mod` | Module definition | Create |
| `main.go` | Entry point | Create |
| `cmd/app.go` | Root CLI command + subcommand registration | Create |
| `cmd/init.go` | `ignk init` command | Create |
| `cmd/lock.go` | `ignk lock` command | Create |
| `cmd/unlock.go` | `ignk unlock` command | Create |
| `cmd/status.go` | `ignk status` command | Create |
| `cmd/list.go` | `ignk list` command | Create |
| `cmd/forget.go` | `ignk forget` command | Create |
| `cmd/lockall.go` | `ignk lock-all` + `ignk unlock-all` commands | Create |
| `internal/core/project.go` | Project root detection, manifest types + R/W | Create |
| `internal/core/vault.go` | Vault resolution, central index types + R/W | Create |
| `internal/core/fileops.go` | Lock/unlock/forget operations, hashing, placeholder generation | Create |
| `internal/ignkfiles/parser.go` | `.ignkfiles` pattern parsing and matching | Create |
| `.gitignore` | Ignore build artifacts | Create |

### Dependencies

- `github.com/urfave/cli/v3` — CLI framework (pin exact version in go.mod)
- `github.com/gofrs/flock` — Cross-platform file locking (flock on Unix, LockFileEx on Windows)
- `github.com/sabhiram/go-gitignore` — `.ignkfiles` pattern matching with full gitignore semantics (replaces doublestar)
- `github.com/natefinch/atomic` — Cross-platform atomic file writes (wraps MoveFileEx on Windows)
- `encoding/json` (stdlib) — Manifest and index serialization (replaces koanf)

### Constraints

- Files-only scope (no directory locking)
- Symlinks as the only unlock mechanism (no copy-swap fallback)
- Vault UID never stored in-project
- Manifest uses project-relative paths only, always forward-slash normalized

---

## Implementation

### Overview

The implementation is structured bottom-up: core types and operations first, then CLI commands that compose them. The package layout:

```
ignk/
├── main.go                         # Entry point
├── cmd/
│   ├── app.go                      # Root command, subcommand wiring
│   ├── init.go                     # ignk init
│   ├── lock.go                     # ignk lock
│   ├── unlock.go                   # ignk unlock
│   ├── status.go                   # ignk status
│   ├── list.go                     # ignk list
│   ├── forget.go                   # ignk forget
│   └── lockall.go                  # ignk lock-all, ignk unlock-all
├── internal/
│   ├── core/
│   │   ├── project.go              # Project detection, manifest
│   │   ├── vault.go                # Vault/index management
│   │   └── fileops.go              # Lock/unlock/forget, hashing, placeholders
│   └── ignkfiles/
│       └── parser.go               # .ignkfiles pattern matching
├── go.mod
└── .gitignore
```

---

### Step 1: Project Scaffolding

**Objective:** Set up Go module, entry point, and dependencies.

**Files:**

- `go.mod`
- `main.go`
- `.gitignore`

**Changes:**

`go.mod`:
```go
module github.com/user/ignk

go 1.23
```

Dependencies added via `go get`:
- `github.com/urfave/cli/v3` (pin exact version)
- `github.com/gofrs/flock`
- `github.com/sabhiram/go-gitignore`
- `github.com/natefinch/atomic`

`main.go`:
```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/user/ignk/cmd"
)

func main() {
    app := cmd.NewApp()
    if err := app.Run(context.Background(), os.Args); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}
```

`.gitignore`:
```
ignk
ignk.exe
```

**Verification:** `go build` compiles without errors.

---

### Step 2: Core Types — Project and Manifest

**Objective:** Define project root detection and manifest data structures.

**Files:**

- `internal/core/project.go`

**Changes:**

Define types:

```go
// Manifest represents .ignk/manifest.json
type Manifest struct {
    Version int                    `json:"version"`
    Files   map[string]*FileEntry  `json:"files"`
}

// FileEntry represents a single managed file
type FileEntry struct {
    State    string `json:"state"`    // "locked" or "unlocked"
    LockedAt string `json:"lockedAt"` // ISO 8601 timestamp
    Hash     string `json:"hash"`     // "sha256:<hex>"
}

// Project represents a detected ignk project
type Project struct {
    Root         string   // Absolute path to project root
    IgnkDir      string   // Absolute path to .ignk/
    ManifestPath string   // Absolute path to .ignk/manifest.json
}
```

Functions:
- `FindProject(startDir string) (*Project, error)` — walk up from `startDir` looking for `.ignk/` directory. Return error if not found.
- `InitProject(dir string) (*Project, error)` — create `.ignk/` and empty `manifest.json` in `dir`.
- `(p *Project) LoadManifest() (*Manifest, error)` — read and parse `manifest.json` using `encoding/json`.
- `(p *Project) SaveManifest(m *Manifest) error` — write `manifest.json` atomically via `atomic.WriteFile` (handles MoveFileEx on Windows).
- `(p *Project) LockManifest() (unlock func(), err error)` — acquire exclusive file lock on `.ignk/manifest.lock` using `gofrs/flock` with a 30-second timeout (`TryLockContext`). Returns unlock function for defer. On timeout, returns error: "could not acquire lock — another ignk operation may be running. If no other operation is active, delete `.ignk/manifest.lock` and retry." No PID-based stale detection — flock auto-releases on process exit.
- `(p *Project) RelPath(absPath string) (string, error)` — convert absolute path to project-relative, forward-slash-normalized path. Uses `filepath.Rel` then `filepath.ToSlash`. Rejects paths that resolve to `..` (outside project root).
- `(p *Project) AbsPath(relPath string) string` — convert forward-slash relative path to OS-native absolute path. Uses `filepath.FromSlash` then `filepath.Join(p.Root, ...)`.

**Path normalization:** All manifest keys use forward slashes regardless of OS. `RelPath` always normalizes via `filepath.ToSlash()`. `AbsPath` always denormalizes via `filepath.FromSlash()`. This matches git's convention and ensures manifests are portable across platforms.

**File locking protocol:** Every command that **modifies** the manifest must:
1. Call `LockManifest()` before `LoadManifest()` — 30-second timeout, actionable error on failure
2. Hold the lock for the entire read-modify-write cycle
3. `defer unlock()` immediately after acquiring
4. No PID-based stale lock detection — OS-level flock auto-releases when the process exits or crashes. On timeout, message instructs user to delete lock file manually if no other operation is active.

**Read-only commands** (`status`, `list`) do NOT acquire the manifest lock. They read the manifest file directly. Atomic writes via `natefinch/atomic` guarantee they see either the old or new version, never a partial write. This prevents read-only commands from being blocked during long batch operations.

**Rationale:** Project detection mirrors git's approach of walking up directories. Atomic manifest writes prevent corruption from crashes. File-based locking prevents corruption from concurrent invocations. Forward-slash normalization ensures cross-platform manifest portability.

**Verification:** Unit test: create temp dir with `.ignk/manifest.json`, verify `FindProject` finds it. Verify `SaveManifest` → `LoadManifest` roundtrip preserves data. Verify `RelPath` produces forward slashes on Windows. Verify `RelPath` rejects `../outside`. Verify concurrent `SaveManifest` calls don't lose updates (run two goroutines writing different keys).

---

### Step 3: Core Types — Vault and Central Index

**Objective:** Define vault resolution and central index management.

**Files:**

- `internal/core/vault.go`

**Changes:**

Define types:

```go
// Index represents ~/.ignk/index.json
type Index struct {
    Version  int                       `json:"version"`
    Projects map[string]*ProjectEntry  `json:"projects"` // UID -> entry
}

// ProjectEntry maps a UID to a project root
type ProjectEntry struct {
    Root         string `json:"root"`
    RegisteredAt string `json:"registeredAt"`
}

// Vault represents a resolved vault for a specific project
type Vault struct {
    UID  string // Short random hex ID
    Dir  string // ~/.ignk/vault/<uid>/
}
```

Functions:
- `IgnkHome() (string, error)` — returns `~/.ignk/`, creating it if it doesn't exist.
- `LockIndex() (unlock func(), err error)` — acquire exclusive file lock on `~/.ignk/index.lock` using `gofrs/flock` with 30-second timeout. Same protocol as `LockManifest`: returns unlock function, no PID-based stale detection.
- `LoadIndex() (*Index, error)` — read `~/.ignk/index.json` using `encoding/json`. Return empty index if file doesn't exist.
- `SaveIndex(idx *Index) error` — write `~/.ignk/index.json` atomically via `atomic.WriteFile`.
- `RegisterProject(projectRoot string) (*Vault, error)` — acquire index lock, look up project in index by root path. If found, return existing vault. If not, generate UID (8 hex chars via `crypto/rand`), create vault dir, add to index, save. Release lock.
- `ResolveVault(projectRoot string) (*Vault, error)` — look up project in index, return vault. Error if not registered.
- `(v *Vault) FilePath(relPath string) string` — return `<vault.Dir>/<filepath.FromSlash(relPath)>`. Converts forward-slash manifest path to OS-native vault path.
- `generateUID() string` — 4 random bytes → 8 hex characters.
- `CheckSymlinkSupport(testDir string) error` — create and remove a test symlink in `testDir` (should be `.ignk/` to test the actual project filesystem, not `os.TempDir()` which may be on a different volume). Returns nil if symlinks work, descriptive error with remediation instructions if not (e.g., "Windows Developer Mode required: Settings > Update & Security > For Developers").

**Rationale:** UID generation uses `crypto/rand` for unpredictability. Index lookup by project root (not UID) means the project never needs to know its own UID. Index locking prevents UID collision from concurrent `init` calls. `CheckSymlinkSupport` catches the Windows Developer Mode requirement before any real operation fails.

**Verification:** Unit test: `RegisterProject` twice with same root returns same UID. Different roots get different UIDs. `ResolveVault` after `RegisterProject` succeeds. Concurrent `RegisterProject` calls for different roots don't lose entries. `FilePath` produces OS-native paths from forward-slash input.

---

### Step 4: Core Operations — Lock, Unlock, Forget

**Objective:** Implement the core file operations.

**Files:**

- `internal/core/fileops.go`

**Changes:**

Functions:

```go
// HashFile computes SHA-256 of a file, returns "sha256:<hex>"
// For files >10MB, prints progress to stderr (bytes hashed / total size)
func HashFile(path string) (string, error)

// GeneratePlaceholder returns placeholder content for a given relative path
func GeneratePlaceholder(relPath string) []byte

// LockFile moves a file to the vault and replaces it with a placeholder
// Warns if file >100MB. Returns error (requiring --force) if file >1GB.
func LockFile(project *Project, vault *Vault, manifest *Manifest, relPath string, force bool) error

// UnlockFile replaces a placeholder with a symlink to the vault copy
// Calls CheckSymlinkSupport on first invocation (cached for session).
func UnlockFile(project *Project, vault *Vault, manifest *Manifest, relPath string) error

// ForgetFile restores a file from vault and removes it from management
func ForgetFile(project *Project, vault *Vault, manifest *Manifest, relPath string) error

// IsPlaceholder checks if a file at the given path is an ignk placeholder
func IsPlaceholder(path string) bool

// FileStatus returns the actual filesystem state of a managed file
func FileStatus(project *Project, vault *Vault, entry *FileEntry, relPath string) string
```

`GeneratePlaceholder` output:
```
[ignk:protected] This file is protected by ignk.
To view its contents, ask the user to run:

    ignk unlock <relPath>

Do NOT attempt to modify or bypass this file.
```

`LockFile` logic:
1. Check manifest — if already locked, return nil (idempotent)
2. Resolve absolute path: `project.AbsPath(relPath)`
3. Verify file exists and is a regular file (not symlink, not dir)
4. **Size check:** `os.Stat` the file. If >100MB, print warning to stderr. If >1GB and `force` is false, return error: "file exceeds 1GB, use --force to lock large files"
5. Compute hash (with progress output for files >10MB)
6. Create vault parent dirs: `os.MkdirAll(filepath.Dir(vault.FilePath(relPath)))`
7. Copy file to vault path (with progress output for files >10MB)
8. Verify vault copy hash matches
9. **Signal safety — point of no return:** only after step 8 confirms vault integrity:
10. Write placeholder over original atomically via `atomic.WriteFile` (handles MoveFileEx on Windows, no manual temp file management needed)
12. Update manifest entry: state="locked", hash, lockedAt=now

Note: the original file is never removed until step 8 confirms the vault copy is intact. If the process is interrupted before step 10, the original file remains untouched. If interrupted between 10 and 11, the file is replaced with a placeholder but the manifest is stale — next `ignk status` detects the inconsistency.

`UnlockFile` logic:
1. **Symlink capability check:** on first call in session, run `CheckSymlinkSupport()`. Cache result. If symlinks unsupported, return error with actionable message (Windows: "enable Developer Mode in Settings > Update & Security > For Developers")
2. Check manifest — if already unlocked, return nil (idempotent)
3. Check manifest — if not managed, return error
4. Verify vault file exists
5. Verify vault file hash matches manifest (warn if mismatch)
6. Check if the file at original path is a placeholder (integrity check — warn if modified)
7. Remove placeholder file
8. Create symlink: original path → vault absolute path
9. Update manifest entry: state="unlocked"

`ForgetFile` logic:
1. Check manifest — if not managed, return error
2. If locked: copy vault file to original path
3. If unlocked: remove symlink, copy vault file to original path
4. Remove vault file (and empty parent dirs)
5. Remove from manifest (in-memory only — caller saves)

`IsPlaceholder`:
- Read first line, check for `[ignk:protected]` prefix

`FileStatus` returns one of:
- `"locked"` — placeholder exists at path, vault file exists
- `"unlocked"` — symlink exists at path pointing to vault
- `"dirty"` — unlocked file has been modified (hash mismatch with manifest)
- `"missing"` — vault file missing
- `"tampered"` — placeholder was overwritten with non-placeholder content
- `"unknown"` — file doesn't exist at original path

**Save protocol — caller-saves only:** All three operations (`LockFile`, `UnlockFile`, `ForgetFile`) modify the in-memory manifest but never call `SaveManifest` themselves. The caller in cmd/ is responsible for saving once after the loop. This ensures consistent behavior and enables partial-failure recovery (see below).

**Partial failure handling:** If a multi-file command (e.g., `ignk lock a.txt b.txt c.txt`) succeeds for `a.txt` and `b.txt` but fails on `c.txt`:
1. Print per-file results as they happen: `locked: a.txt`, `locked: b.txt`, `error: c.txt: <reason>`
2. Save the manifest with all successfully completed operations (`a.txt` and `b.txt` tracked)
3. Return an error indicating partial completion: "2 of 3 files locked, 1 failed"
This prevents orphaned files (locked on disk but absent from manifest).

**Signal handling:** Batch commands (`lock`, `unlock`, `forget`, `lock-all`, `unlock-all`) register a SIGINT handler that saves the in-memory manifest before exiting. If user presses Ctrl+C after locking 8 of 10 files, the manifest reflects files 1-8 correctly.

**Display paths:** All terminal output (per-file results, status table, list output) displays OS-native paths via `filepath.FromSlash(relPath)`. On Windows, users see `config\secrets.yaml`; on Unix, `config/secrets.yaml`. Internal storage always uses forward slashes.

**Rationale:** Every destructive operation (replacing original file) is preceded by a successful vault write. Atomic rename prevents partial states. Signal safety ensures Ctrl+C during lock never loses the original file. Symlink capability check catches Windows Developer Mode issues before any filesystem mutation. Caller-saves protocol prevents the inconsistency of some operations saving internally and others not.

**Verification:** Integration test: create a temp file, lock it, verify placeholder exists and vault copy matches. Unlock it, verify symlink points to vault. Lock again, verify idempotent. Forget, verify original restored. Partial failure test: lock three files where the third doesn't exist — verify first two are tracked in manifest.

---

### Step 5: `.ignkfiles` Parser

**Objective:** Parse `.ignkfiles` patterns and match against project files.

**Files:**

- `internal/ignkfiles/parser.go`

**Changes:**

`.ignkfiles` format — full gitignore semantics:
```
# Comments (lines starting with #)
# Empty lines ignored
# Full gitignore pattern syntax via go-gitignore:
#   * matches any sequence of non-separator characters
#   ** matches zero or more directories
#   ? matches a single non-separator character
#   ! negates a pattern (last matching pattern wins)
#   Slash-less patterns match at any depth (*.pem matches config/ssl/server.pem)
#   Leading / anchors to project root

.env
.env.*
*.pem
config/secrets.yaml
!config/secrets.example.yaml
```

Note: `*.pem` (no slash) matches `.pem` files at any depth — this is gitignore behavior. In doublestar, you'd need `**/*.pem`. go-gitignore handles this natively.

Functions:
```go
// Load reads a .ignkfiles file and compiles the patterns
func Load(path string) (*ignore.GitIgnore, error)

// DiscoverFiles walks the project tree and returns all files matching .ignkfiles patterns
// Excludes .ignk/ directory and already-managed files
func DiscoverFiles(projectRoot string, ignorer *ignore.GitIgnore, manifest *core.Manifest) ([]string, error)
```

`Load` logic:
- Use `ignore.CompileIgnoreFile(path)` from `go-gitignore`
- Returns compiled matcher that handles all gitignore semantics: `**`, `!`, slash-less basename matching, anchoring
- Returns error if file can't be read

`DiscoverFiles` logic:
- `filepath.WalkDir` from project root
- Skip `.ignk/`, `.git/`, and other dotdirs
- For each regular file, compute relative path (forward-slash normalized via `filepath.ToSlash`)
- Call `ignorer.MatchesPath(relPath)` — returns true if file matches patterns
- Exclude files already in manifest
- Return list of relative paths (forward-slash normalized)

**Rationale:** `go-gitignore` replaces `doublestar` because doublestar is a general-purpose glob library that doesn't implement gitignore's "slash-less pattern matches at any depth" rule. With doublestar, `*.pem` only matches root-level `.pem` files — `config/ssl/server.pem` would require `**/*.pem`. Since `.ignkfiles` is documented as "like .gitignore" and users will copy patterns from their `.gitignore` experience, the matching library must implement actual gitignore semantics. `go-gitignore` does this natively: slash-less patterns, `**` globstar, `!` negation, `/` anchoring — all handled correctly.

**Verification:** Unit test: verify `*.pem` matches BOTH `key.pem` AND `config/ssl/server.pem` (gitignore's slash-less = match anywhere). Verify `**/*.pem` also works. Verify `config/*.yaml` matches `config/secrets.yaml` but NOT `config/sub/deep.yaml`. Test negation (`!config/secrets.example.yaml`). Test anchoring (`/root-only.env` matches only at root). Test empty file. Test comments.

---

### Step 6: CLI Commands — `init`

**Objective:** Wire up `ignk init` command.

**Files:**

- `cmd/app.go`
- `cmd/init.go`

**Changes:**

`cmd/app.go` — root command with all subcommands registered:
```go
func NewApp() *cli.Command {
    return &cli.Command{
        Name:    "ignk",
        Usage:   "Protect sensitive files from AI coding agents",
        Commands: []*cli.Command{
            initCmd(),
            lockCmd(),
            unlockCmd(),
            statusCmd(),
            listCmd(),
            forgetCmd(),
            lockAllCmd(),
            unlockAllCmd(),
        },
    }
}
```

`cmd/init.go`:
- Get current working directory
- Check if already initialized (`.ignk/` exists) — warn and exit
- **Symlink capability check:** call `core.CheckSymlinkSupport()`. If fails, print warning (not error) — init can proceed, but unlock won't work. Message: "Warning: symlinks not supported on this system. ignk unlock will not work until Developer Mode is enabled (Windows: Settings > Update & Security > For Developers)."
- Call `core.InitProject(cwd)`
- Call `core.RegisterProject(cwd)` (acquires index lock internally)
- Print: `Initialized ignk in <dir>` and `Vault: <vault.Dir>`

**Verification:** Run `ignk init` in a temp dir. Verify `.ignk/manifest.json` created. Verify `~/.ignk/index.json` updated. Run again — should warn "already initialized". On Windows without Developer Mode — should print symlink warning but succeed.

---

### Step 7: CLI Commands — `lock` and `unlock`

**Objective:** Wire up `ignk lock` and `ignk unlock` commands.

**Files:**

- `cmd/lock.go`
- `cmd/unlock.go`

**Changes:**

`cmd/lock.go`:
- Find project (`core.FindProject`)
- Resolve vault (`core.ResolveVault`)
- **Acquire manifest lock** (`project.LockManifest()`, defer unlock)
- Load manifest
- Register SIGINT handler that saves manifest before exit
- For each arg: resolve to relative path (via `project.RelPath` — forward-slash normalized), call `core.LockFile`
- Print per-file result as it happens: `locked: <path>` or `already locked: <path>` or `error: <path>: <reason>`
- Save manifest once at end (including on partial failure — save successful ops)
- If any file failed, return error: "N of M files locked, K failed"
- Accepts `--force` flag to allow locking files >1GB

`cmd/unlock.go`:
- Find project (`core.FindProject`)
- Resolve vault (`core.ResolveVault`)
- **Acquire manifest lock** (`project.LockManifest()`, defer unlock)
- Load manifest
- Register SIGINT handler that saves manifest before exit
- For each arg: resolve to relative path (via `project.RelPath`), call `core.UnlockFile`
- Print per-file result as it happens: `unlocked: <path>` or `already unlocked: <path>` or `error: <path>: <reason>`
- Save manifest once at end (including on partial failure — save successful ops)
- If any file failed, return error: "N of M files unlocked, K failed"

Both commands accept variadic path arguments (`cmd.Args().Slice()`).

**Verification:** Create test file, `ignk lock test.txt`, verify placeholder. `ignk unlock test.txt`, verify symlink. Verify idempotent behavior.

---

### Step 8: CLI Commands — `status` and `list`

**Objective:** Wire up `ignk status` and `ignk list` commands.

**Files:**

- `cmd/status.go`
- `cmd/list.go`

**Changes:**

`cmd/status.go`:
- Find project, resolve vault, load manifest (**no manifest lock** — read-only command)
- For each manifest entry: call `core.FileStatus`
- Print table:
  ```
  locked      .env
  unlocked    config/secrets.yaml
  tampered    old-key.pem
  missing     lost-file.txt
  ```
- Color-code if terminal supports it (optional, can skip for MVP)

`cmd/list.go`:
- Find project, load manifest (**no manifest lock** — read-only command)
- Print each managed file path, one per line (OS-native paths via `filepath.FromSlash`)
- Simpler than status — just filenames, no state checking

**Verification:** Lock some files, unlock others. Run `ignk status` — verify output matches. Run `ignk list` — verify all managed files shown.

---

### Step 9: CLI Commands — `forget`

**Objective:** Wire up `ignk forget` command.

**Files:**

- `cmd/forget.go`

**Changes:**

- Find project, resolve vault
- **Acquire manifest lock** (`project.LockManifest()`, defer unlock)
- Load manifest
- Register SIGINT handler that saves manifest before exit
- For each arg: resolve to relative path (via `project.RelPath`), call `core.ForgetFile`
- Print per-file result as it happens: `forgot: <path> (restored to original location)` or `error: <path>: <reason>`
- Save manifest once at end (including on partial failure)
- If any file failed, return error: "N of M files forgotten, K failed"

**Verification:** Lock a file, then `ignk forget` it. Verify original file restored at original path, removed from manifest and vault.

---

### Step 10: CLI Commands — `lock-all` and `unlock-all`

**Objective:** Wire up bulk operations.

**Files:**

- `cmd/lockall.go`

**Changes:**

`lock-all`:
- Find project, resolve vault
- **Acquire manifest lock** (`project.LockManifest()`, defer unlock)
- Load manifest
- Parse `.ignkfiles` if it exists (using `ignkfiles.Load`)
- Discover new files matching patterns (`ignkfiles.DiscoverFiles`)
- **`--dry-run` mode:** if flag set, print list of files that would be locked and exit without locking. No filesystem mutations.
- Otherwise: lock all (already-managed unlocked files + newly discovered files)
- Register SIGINT handler that saves manifest before exit
- Print per-file result as it happens
- Save manifest once at end (including on partial failure)
- Accepts `--force` flag for large files
- Print summary: `locked N files (M new, K re-locked)`

`unlock-all`:
- Find project, resolve vault
- **Acquire manifest lock** (`project.LockManifest()`, defer unlock)
- Load manifest
- Register SIGINT handler that saves manifest before exit
- For each locked entry in manifest: call `core.UnlockFile` (first call checks symlink capability)
- Print per-file result as it happens
- Save manifest once at end (including on partial failure)
- Print summary: `unlocked N files`

**Verification:** Create `.ignkfiles` with patterns, create matching files. Run `ignk lock-all` — verify all matched files locked. Run `ignk unlock-all` — verify all unlocked.

---

### Step 11: Build Verification

**Objective:** Ensure the binary builds and runs on the development machine.

**Files:** None (verification step only)

**Changes:**

```bash
go build -o ignk .
```

Run through the full workflow:
```bash
mkdir testproject && cd testproject
ignk init
echo "SECRET=abc123" > .env
echo "password: hunter2" > secrets.yaml
ignk lock .env secrets.yaml
cat .env                    # Should show placeholder
ignk status                 # Should show both locked
ignk unlock .env            # Should create symlink
cat .env                    # Should show SECRET=abc123
ignk lock-all               # Should re-lock .env
ignk forget secrets.yaml    # Should restore original
ignk list                   # Should show only .env
```

**Verification:** All commands execute without error. File states match expectations.

---

## Verification Plan

### Automated Checks

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues

### Manual Verification

- [ ] Full workflow test (init → lock → unlock → status → list → forget → lock-all → unlock-all)
- [ ] Idempotency: double-lock and double-unlock produce no errors
- [ ] Placeholder content contains actual file path
- [ ] Symlink target points to correct vault path
- [ ] Hash mismatch detected if vault file is manually altered
- [ ] `.ignkfiles` patterns correctly discover files — `*.pem` matches `config/ssl/server.pem` (gitignore slash-less semantics)
- [ ] Works from subdirectory (project root detected by walking up)
- [ ] Concurrent `ignk lock` in two terminals doesn't lose manifest entries
- [ ] `ignk lock ../outside-project/file` is rejected
- [ ] Manifest paths use forward slashes regardless of OS
- [ ] On Windows without Developer Mode: `ignk init` warns, `ignk unlock` fails with clear message
- [ ] Large file (>100MB) produces warning; >1GB without `--force` produces error
- [ ] Ctrl+C during `ignk lock` does not lose the original file
- [ ] Ctrl+C during `ignk lock-all` (after N files) saves manifest with N files tracked
- [ ] Partial failure: `ignk lock a.txt b.txt nonexistent.txt` locks a.txt and b.txt, saves both to manifest, reports error for nonexistent.txt
- [ ] `ignk lock-all --dry-run` lists files without locking them
- [ ] `ignk status` does not hang while `ignk lock-all` is running in another terminal
- [ ] Lock acquisition timeout: `ignk lock` prints actionable message if lock held >30s

### Acceptance Criteria Validation

| Criterion | How to Verify | Expected Result |
|-----------|---------------|-----------------|
| Lock moves file to vault | Check vault dir after `ignk lock` | File exists in `~/.ignk/vault/<uid>/<path>` |
| Placeholder content correct | `cat` a locked file | Shows `[ignk:protected]` with correct path |
| Unlock creates symlink | `ls -la` (or `dir`) an unlocked file | Shows symlink → vault path |
| Forget restores original | `cat` a forgotten file | Shows original content, not in manifest |
| Cross-platform symlinks | `go build` + test on Windows | Symlinks created via `os.Symlink` |

---

## Rollback Plan

Greenfield project — if implementation fails, delete all files and start over. No existing code to break.

---

## Notes

### Assumptions

- Go 1.23+ is available on the development machine
- Symlink creation may require Developer Mode on Windows — detected at init/unlock with actionable error
- `~/.ignk/` is a writable location on all target platforms. Default location; future `$IGNK_HOME` env var override planned for post-MVP

### Known Limitations (documented in red team)

- **Symlink target path leakage:** when a file is unlocked, `ls -la` or `readlink` reveals the vault path including UID (e.g., `~/.ignk/vault/a1b2c3d4/.env`). This weakens the "opaque UID" protection claim from the proposal. Protection against vault discovery is only effective in the locked state. This is an inherent limitation of symlinks and is accepted for MVP.
- **Unlocked = fully exposed:** when unlocked, any process (including AI agents) reads the file transparently through the symlink. Protection exists only in the locked state. This is by design but must be clearly communicated.
- **No copy-swap fallback:** Windows users without Developer Mode cannot unlock files. Detected and surfaced as a clear error, but no workaround exists in MVP.

### Risks

- **urfave/cli v3 API instability:** v3 may still be pre-release. Mitigation: pin exact version in go.mod, vendor dependencies.

### Open Questions

(none — all decisions made, red team pass 1 + pass 2 findings addressed)
