# ignk — Symlink-Based Sensitive File Protection for AI Agents

> **Status:** Accepted
> **Created:** 2026-02-11
> **Author:** Claude (agent) + user
> **Related Projex:** `20260211-ignk-mvp-plan.md`

---

## Summary

`ignk` is a CLI tool that protects sensitive files from AI coding agents by replacing them with placeholder stubs and storing the originals in a centralized vault outside the project tree. When an AI agent reads a protected path, it sees only a message instructing the user to run `ignk unlock <actual-path>`. Unlocking swaps the placeholder for a symlink to the vault copy, granting access on demand.

---

## Problem Statement

### Current State

AI coding agents (Claude Code, Cursor, Copilot, Windsurf, Aider, etc.) read project files to provide assistance. Each tool has its own ignore mechanism (`.gitignore`, `.cursorignore`, `.claudeignore`, etc.), but:

- Not all tools implement ignore mechanics properly or at all
- New tools arrive constantly with no ignore support on day one
- Ignore files are tool-specific — there's no universal standard
- Some agents follow symlinks, read broadly, or index everything in the project tree
- Sensitive files (credentials, API keys, private configs, proprietary code) sit in the working tree fully readable

### Gap / Need / Opportunity

There is no **tool-agnostic, agent-agnostic** mechanism to prevent AI agents from reading specific files. The protection needs to work at the filesystem level — before any agent's ignore logic even runs — by ensuring the sensitive content simply isn't there to be read.

### Why Now?

AI coding agents are proliferating rapidly. Users routinely switch between multiple agents or run them in parallel. Relying on each agent's bespoke ignore config is fragile and error-prone. A filesystem-level solution is both timelier and more durable than chasing per-tool configs.

---

## Proposed Change

### Overview

`ignk` manages two states for each protected file:

| State | What's at the original path | Real file location |
|-------|----------------------------|-------------------|
| **Locked** (default) | A small placeholder text file | Stored in centralized vault (`~/.ignk/vault/...`) |
| **Unlocked** | A symlink pointing to the vault copy | `~/.ignk/vault/<full-original-path>` |

The placeholder content is deliberately designed to be understood by AI agents:

```
[ignk:protected] This file is protected by ignk.
To view its contents, ask the user to run: ignk unlock <actual-path>
```

### Centralized Vault with UID Isolation

The vault lives **outside all project trees** at `~/.ignk/vault/`. Each project gets a unique ID, and files are stored under that UID — inspired by how Sandboxie virtualizes filesystem paths, but without exposing the real project path:

```
~/.ignk/
├── index.json              # Maps UIDs ↔ project roots
└── vault/
    ├── a1b2c3d4/           # UID for S:\Repos\ignk
    │   ├── .env
    │   └── config/
    │       └── secrets.yaml
    └── f5e6d7c8/           # UID for S:\Repos\other-project
        └── .env
```

This design provides three layers of protection:

1. **Out-of-tree** — AI agents scanning the project directory never encounter the vault
2. **Opaque UIDs** — symlink targets (e.g., `~/.ignk/vault/a1b2c3d4/.env`) don't reveal the project path or allow prediction of other projects' vault locations
3. **No in-project vault references** — the project tree contains only `manifest.json` (file list + states) and placeholder files; the vault UID never appears in-project, so there's no breadcrumb for agents to follow
4. **Relocation-friendly** — moving a project only requires updating `~/.ignk/index.json`, not restructuring the vault

### Core Commands

```
ignk init                    # Initialize .ignk/ in project root
ignk lock <path>...          # Move file(s) to vault, replace with placeholder
ignk unlock <actual-path>...        # Replace placeholder with symlink to vault copy
ignk status                  # Show lock state of all managed files
ignk list                    # List all managed files and their states
ignk forget <path>...        # Unmanage file(s), restore original in place
ignk lock-all                # Lock all managed files
ignk unlock-all              # Unlock all managed files
```

### Recommended Approach

**Symlinks** as the primary mechanism. Rationale:

1. Symlinks are the correct abstraction — they're atomic, don't duplicate data, and changes to the real file are instantly visible through the link
2. Symlink creation works on modern Windows, macOS, and Linux without special setup
3. No sync problem — the vault IS the single source of truth, the symlink just provides access
4. A copy-swap fallback (`--no-symlink`) can be added later if needed

---

## Design Details

### In-Project Structure

Only metadata lives inside the project — no secrets, no vault identifiers:

```
.ignk/
└── manifest.json           # Tracks managed files and their states
```

The vault UID is **never stored in-project**. `ignk` resolves the vault at runtime by looking up the current project root in the central `~/.ignk/index.json`. This ensures that nothing inside the project tree can lead an agent to the vault location.

### Manifest Schema

```json
{
  "version": 1,
  "files": {
    "relative/path/to/file": {
      "state": "locked | unlocked",
      "lockedAt": "2026-02-11T12:00:00Z",
      "hash": "sha256:abc123..."
    }
  }
}
```

The manifest uses **project-relative paths** only. The vault location is computed at runtime from the project root's absolute path. This means the manifest is portable and committable.

### Placeholder File Format

```
[ignk:protected] This file is protected by ignk.
To view its contents, ask the user to run:

    ignk unlock config/secrets.yaml

Do NOT attempt to modify or bypass this file.
```

The placeholder is deliberately plain text (not JSON/YAML/code) so any agent that reads it — regardless of the file's original extension — will see the human-readable instruction and relay it to the user.

### Safety Invariants

1. **No data loss** — `lock` must verify vault write succeeded before replacing original
2. **Atomic operations** — Use temp files + rename to prevent partial states
3. **Hash verification** — Record SHA-256 on lock, verify on unlock to detect vault tampering
4. **Idempotent commands** — `lock` on an already-locked file is a no-op, not an error
5. **Dirty check** — Warn if an unlocked file was modified before re-locking (i.e., the working copy differs from vault)
6. **Placeholder integrity** — Detect if a placeholder has been overwritten (by an agent or user) before unlocking

### Integration Points

- **`.gitignore`** — Placeholder files are committed; no vault content is in-tree to ignore
- **`.ignk/manifest.json`** — Should be committed so team members know which files are managed
- **Placeholder files** — Should be committed so cloned repos show the protection stubs

### Technology Choices (Decided)

- **Language:** Go — single binary, fast, good cross-platform symlink support, simple distribution
- **Distribution:** Compiled binaries via GitHub Releases (per-platform)
- **File selection:** Explicit paths (`ignk lock .env`) + `.ignkfiles` declarative patterns (`.gitignore`-style)
- **Scope:** Files only (no directory locking)

---

## Impact Analysis

### Affected Areas

- **Project filesystem** — files are moved/replaced; build tools, editors, and agents see either placeholders or symlinks
- **Git workflow** — placeholder files are committed; vault is external and never touched by git
- **CI/CD** — locked files won't have real content; CI must either unlock or use its own secrets management
- **Team workflow** — vault is per-machine; team members need ignk installed and must source secrets independently (correct behavior — secrets shouldn't travel via git)

### Dependencies

- Symlink support on the host OS
- No external services or network access required

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| AI agent follows/resolves symlinks when unlocked | Medium | Medium | By design — unlock grants access; lock when done |
| AI agent overwrites placeholder with generated content | Medium | Medium | Placeholder integrity check on unlock; warn user |
| User forgets to re-lock before committing | Medium | High | Pre-commit hook (`ignk check`) that warns if unlocked files are staged |
| Project directory is moved/renamed | Low | Low | Vault uses UIDs, not project paths; index auto-updates on next command |
| Vault deletion | Low | Critical | Hash verification; clear error messages; vault is just files — normal backup practices apply |
| Build tools break on placeholder files | Medium | Medium | Placeholder matches no valid syntax — build fails loud, not silent |

### Breaking Changes

None — greenfield project.

---

## Known Limitations

### Project Relocation

If the project directory moves, symlinks pointing to the vault still work (vault paths use UIDs, not project paths). However, the central `~/.ignk/index.json` mapping becomes stale. Running any `ignk` command from the new location auto-updates the index. Dangling symlinks in the project need re-creation — `ignk unlock --refresh` can handle this.

### Protection is Locked-State Only

When a file is unlocked (symlink active), any agent can read through the symlink transparently. Protection only exists in the locked state. This is a deliberate "default deny" design — the user explicitly grants access and is responsible for re-locking.

### Filename Visibility

Locking protects file *content*, not file *existence*. Directory listings still show the filename (e.g., `.env`, `secrets.yaml`). An agent can infer that secrets exist even if it can't read them.

### Per-Machine Vault

The vault doesn't travel with the project. New team members or fresh clones see placeholders but must source the real files independently. This is the correct security behavior but adds onboarding friction.

---

## Open Questions (Resolved)

- [x] What language/runtime? → **Go**, compiled binaries via GitHub Releases
- [x] Glob patterns? → **Yes**, via `.ignkfiles` (`.gitignore`-style declarative file)
- [x] Git hooks? → **Deferred** (post-MVP)
- [x] File extension? → **Preserve original extension** (placeholder replaces content, keeps the filename)
- [x] Directories? → **Files only**
- [x] Declarative file selection? → **Yes**, `.ignkfiles` in project root

---

## Next Steps

If accepted:
1. Decide on language/runtime
2. Create a Plan projex for MVP implementation (init, lock, unlock, status, list, forget)
3. Implement and test on Windows/macOS/Linux
4. Add git hook support as a fast-follow

---

## Appendix

### Threat Model

`ignk` protects against **opportunistic reads by AI agents**, not against determined adversaries. It is a *courtesy lock*, not a security boundary. Specifically:

| Threat | Protected? | Notes |
|--------|-----------|-------|
| AI agent reads file content | Yes (when locked) | Agent sees placeholder text |
| AI agent lists directory | Partial | Filename visible, content protected |
| AI agent resolves symlinks | No (when unlocked) | By design — unlock grants access |
| AI agent overwrites placeholder | Detectable | Integrity check warns user on next unlock |
| Malicious human user | No | They can read the vault directly |
| Compromised build system | No | Out of scope — use proper secrets management |

### Prior Art / Alternatives Considered

1. **Per-tool ignore files** (`.gitignore`, `.cursorignore`, `.claudeignore`) — Tool-specific, not universal; doesn't help with tools that lack ignore support
2. **git-crypt / SOPS** — Encrypts files in git; doesn't prevent AI agents from reading decrypted working copies
3. **Environment variables only** — Good practice but not always feasible for complex configs, certificates, or proprietary code files
4. **`.env` + `.gitignore`** — Only protects from git commits, not from local file reads by agents
5. **File permissions (chmod)** — Agents typically run as the same user; no protection
6. **In-project vault** (original design) — Vault inside project tree is discoverable by the same agents ignk aims to block; rejected in favor of centralized external vault
7. **Full-path vault mirroring** — Vault mirrors original absolute paths (Sandboxie-style); rejected because symlink targets expose project paths and make vault locations predictable across projects; replaced with UID-based vault isolation
