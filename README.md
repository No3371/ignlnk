# ignlnk

**Protect sensitive files from AI coding agents.**

ignlnk is a CLI tool that shields files you don't want AI agents to read or modify — API keys, credentials, proprietary configs, personal notes — by replacing them with inert placeholder stubs and storing the originals in a secure vault outside your project tree.

When *you* need the real files back, a single command restores access via symlinks. Lock before handing off to an agent; unlock when you're done.

## The Problem

AI coding agents (Copilot, Cursor, Goose, Aider, etc.) typically have read access to your entire project directory. There's no standard mechanism to tell them "don't look at this file." `.gitignore` only controls version control — agents ignore it. Sensitive files sitting in your working tree are fair game.

ignlnk solves this by **physically removing** sensitive files from the project tree during agent sessions, replacing them with harmless placeholders that contain no real content.

## How It Works

Each managed file has two states:

| State | In Project Tree | In Vault | Agent Sees |
|---|---|---|---|
| **Locked** | Placeholder stub | Original file | A text file saying `"this file is managed by ignlnk"` |
| **Unlocked** | Symlink → vault | Original file | Real content (via symlink) |

```
# Locked state (safe for agents)
myproject/
  .env              ← placeholder (inert text)
  
~/.ignlnk/vault/<uid>/
  .env              ← real file (agent can't reach here)

# Unlocked state (you're working)
myproject/
  .env              ← symlink → ~/.ignlnk/vault/<uid>/.env
```

The vault lives at `~/.ignlnk/vault/`, with each project isolated by a unique ID. A manifest (`.ignlnk/manifest.json`) tracks managed files and their states.

## Installation

### Using `go install` (Recommended)

Requires **Go 1.24+**.

```bash
go install github.com/No3371/ignlnk@latest
```

This installs the binary to `$GOPATH/bin` (typically `~/go/bin` or `%USERPROFILE%\go\bin` on Windows). Make sure this directory is in your `PATH`.

### From Source

```bash
git clone https://github.com/No3371/ignlnk.git
cd ignlnk
go build -o ignlnk .
```

Move the binary to somewhere on your `PATH`:

```bash
# Linux / macOS
mv ignlnk /usr/local/bin/

# Windows (PowerShell)
Move-Item ignlnk.exe C:\Users\<you>\bin\
```

## Quick Start

```bash
# 1. Initialize ignlnk in your project
cd myproject
ignlnk init

# 2. Lock sensitive files before an agent session
ignlnk lock .env secrets/api-key.json

# 3. Run your AI agent — it only sees placeholders
#    ... agent does its work ...

# 4. Unlock when you need the real files back
ignlnk unlock .env secrets/api-key.json
```

### Bulk Operations with `.ignlnkfiles`

Create a `.ignlnkfiles` file in your project root to define patterns (same syntax as `.gitignore`):

```gitignore
# .ignlnkfiles
.env
.env.*
secrets/
*.pem
*.key
config/credentials.*
```

Then lock/unlock everything at once:

```bash
ignlnk lock-all          # Lock all files matching patterns
ignlnk unlock-all        # Unlock all managed files
```

## Command Reference

| Command | Description |
|---|---|
| `ignlnk init` | Initialize ignlnk in the current directory. Creates `.ignlnk/` and registers the project in the central vault. |
| `ignlnk lock <path>...` | Lock one or more files — moves originals to vault, replaces with placeholders. Use `--force` for files >1 GB. |
| `ignlnk unlock <path>...` | Unlock one or more files — replaces placeholders with symlinks to vault copies. |
| `ignlnk lock-all` | Lock all files matching `.ignlnkfiles` patterns. Use `--force` for files >1 GB. |
| `ignlnk unlock-all` | Unlock all currently locked managed files. |
| `ignlnk status` | Show all managed files and their current state (locked, unlocked, or anomalies). |
| `ignlnk list` | List all managed file paths. |
| `ignlnk forget <path>...` | Stop managing files — restores originals from vault and removes from manifest. |

## `.ignlnkfiles` Pattern File

The `.ignlnkfiles` file uses `.gitignore`-style glob patterns to define which files should be managed. It is used by `lock-all` to discover files.

- One pattern per line
- Lines starting with `#` are comments
- Supports `*`, `**`, and directory patterns (trailing `/`)
- Dot-directories (`.git/`, `.ignlnk/`, etc.) are always skipped

```gitignore
# Secrets
.env
.env.*
secrets/**

# Certificates
*.pem
*.key
*.p12

# IDE credentials
.vscode/settings.json
```

## Platform Requirements

### Symlinks

The **unlock** operation creates symlinks. This requires:

- **Linux / macOS**: Works out of the box.
- **Windows**: Requires **Developer Mode** enabled (Settings → Update & Security → For Developers), or running as Administrator.

If symlink support is unavailable, `ignlnk init` will print a warning. Locking still works — you just won't be able to unlock until symlinks are enabled.

### Locking (always works)

Lock replaces files with plaintext placeholders and does **not** require symlink support. This is the operation that matters for protecting files from agents.

## Project Structure

```
.ignlnk/                  ← Created by `ignlnk init`
  manifest.json            ← Tracks managed files, states, hashes
  manifest.lock            ← File lock for concurrent safety
.ignlnkfiles               ← Your pattern file (optional, you create this)

~/.ignlnk/                 ← Central vault (outside project tree)
  index.json               ← Maps project roots to vault UIDs
  vault/<uid>/             ← Per-project vault directory
    path/to/file           ← Original files, mirroring project structure
```

## Safety

ignlnk is designed to never lose your data:

- **Atomic operations**: File moves use rename where possible; cross-device moves copy-then-remove with hash verification.
- **Manifest locking**: A file lock prevents concurrent mutations from corrupting state.
- **Signal handling**: Graceful manifest save on SIGINT/SIGTERM during batch operations.
- **Hash verification**: SHA-256 checksums are stored in the manifest and verified during unlock/forget to detect corruption.

## Known Limitations

- **One vault location**: The vault is always at `~/.ignlnk/vault/` — not configurable yet.
- **No encryption**: Vault files are stored in plaintext. The vault provides *isolation*, not *encryption*.
- **Symlink visibility**: Some tools follow symlinks transparently, so an unlocked file's content is fully accessible. Only the **locked** state truly hides content.
- **No `.gitignore` auto-sync**: You should manually add `.ignlnk/` to your `.gitignore`.
- **Git operations**: Locking/unlocking changes the working tree. Commit or stash before bulk operations if you have uncommitted changes.

## Recommended `.gitignore` Addition

```gitignore
# ignlnk project data
.ignlnk/
```

## Contributing

Contributions are welcome! The project is written in Go and uses the [urfave/cli](https://github.com/urfave/cli) framework.

```bash
# Run tests
go test ./...

# Build
go build -o ignlnk .
```

## License

See [LICENSE](LICENSE) for details.
