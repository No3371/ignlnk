# ignlnk Manual Test Procedure

> **Purpose:** Reproducible end-to-end verification of ignlnk CLI across environments.
> **Prerequisites:** Go 1.24+, symlink support (Windows: Developer Mode enabled)
> **Duration:** ~5 minutes
> **Related:** `20260211-ignlnk-mvp-plan.md`, `20260211-ignlnk-mvp-walkthrough.md`

---

## Environment Setup

### 1. Build

```bash
go build -o ignlnk .          # Unix
go build -o ignlnk.exe .      # Windows
go vet ./...                   # Must report no issues
```

### 2. Create Isolated Test Directory

```bash
mkdir /tmp/ignlnk-test && cd /tmp/ignlnk-test
```

Use a directory **outside** the source repo to avoid interference.

### 3. Clean Prior State (if re-running)

```bash
rm -rf /tmp/ignlnk-test/*
rm -rf /tmp/ignlnk-test/.ignlnk
rm -rf /tmp/ignlnk-test/.ignlnkfiles
rm -rf /tmp/ignlnk-test/.env*
```

Also remove the test project's vault entry from the central index if it exists:
```bash
cat ~/.ignlnk/index.json    # find the UID for the test directory
# Manually remove entry, or delete ~/.ignlnk/ entirely for a fresh start
```

---

## Test Cases

> **Convention:** `$IGNLNK` refers to the path to the built binary.
> Adjust path separators for your OS — expected outputs below use Unix style.
> Windows will show backslashes in output (e.g., `config\ssl\server.pem`).

---

### T01: Init

```bash
$IGNLNK init
```

**Expected output:**
```
Initialized ignlnk in <test-dir>
Vault: <home>/.ignlnk/vault/<uid>
```

**Verify:**
- `.ignlnk/manifest.json` exists and contains `{"version":1,"files":{}}`
- `~/.ignlnk/index.json` contains an entry mapping a UID to the test directory
- `~/.ignlnk/vault/<uid>/` directory exists

---

### T02: Init Idempotency

```bash
$IGNLNK init
```

**Expected output (stderr):**
```
warning: already initialized
```

**Verify:** No error, no duplicate index entries.

---

### T03: Lock Files

```bash
echo "SECRET=abc123" > .env
echo "password: hunter2" > secrets.yaml
$IGNLNK lock .env secrets.yaml
```

**Expected output:**
```
locked: .env
locked: secrets.yaml
```

**Verify:**
- `cat .env` shows placeholder:
  ```
  [ignlnk:protected] This file is protected by ignlnk.
  To view its contents, ask the user to run:

      ignlnk unlock .env

  Do NOT attempt to modify or bypass this file.
  ```
- `cat secrets.yaml` shows equivalent placeholder with `secrets.yaml` path
- Vault copies exist: `~/.ignlnk/vault/<uid>/.env` and `~/.ignlnk/vault/<uid>/secrets.yaml`
- Vault copies contain original content
- `.ignlnk/manifest.json` has entries for both files with `"state":"locked"` and `"hash":"sha256:..."`

---

### T04: Lock Idempotency

```bash
$IGNLNK lock .env
```

**Expected output:**
```
already locked: .env
```

**Verify:** No error, no changes to manifest or filesystem.

---

### T05: Status (All Locked)

```bash
$IGNLNK status
```

**Expected output:**
```
locked      .env
locked      secrets.yaml
```

---

### T06: Unlock

```bash
$IGNLNK unlock .env
```

**Expected output:**
```
unlocked: .env
```

**Verify:**
- `cat .env` shows `SECRET=abc123` (original content)
- File is a symlink: `ls -la .env` (Unix) or `dir .env` (Windows) shows symlink arrow
- Symlink target is `~/.ignlnk/vault/<uid>/.env`
- `.ignlnk/manifest.json` entry for `.env` has `"state":"unlocked"`

---

### T07: Unlock Idempotency

```bash
$IGNLNK unlock .env
```

**Expected output:**
```
already unlocked: .env
```

---

### T08: Status (Mixed States)

```bash
$IGNLNK status
```

**Expected output:**
```
unlocked    .env
locked      secrets.yaml
```

---

### T09: Lock-All (Re-lock Unlocked Files)

```bash
$IGNLNK lock-all
```

**Expected output:**
```
locked: .env
locked 1 files (0 new, 1 re-locked)
```

**Verify:**
- `cat .env` shows placeholder again (no longer a symlink)
- `$IGNLNK status` shows both locked

---

### T10: Forget

```bash
$IGNLNK forget secrets.yaml
```

**Expected output:**
```
forgot: secrets.yaml (restored to original location)
```

**Verify:**
- `cat secrets.yaml` shows `password: hunter2` (original content)
- `secrets.yaml` is a regular file, not a symlink or placeholder
- `$IGNLNK list` does **not** include `secrets.yaml`
- Vault copy removed: `~/.ignlnk/vault/<uid>/secrets.yaml` does not exist

---

### T11: List

```bash
$IGNLNK list
```

**Expected output:**
```
.env
```

Only `.env` remains managed (secrets.yaml was forgotten).

---

### T12: Unlock-All

```bash
$IGNLNK unlock-all
```

**Expected output:**
```
unlocked: .env
unlocked 1 files
```

**Verify:** `cat .env` shows `SECRET=abc123`.

---

### T13: .ignlnkfiles Pattern Matching

**Setup:**
```bash
cat > .ignlnkfiles << 'EOF'
*.pem
.env.*
EOF
mkdir -p config/ssl
echo "cert-data" > config/ssl/server.pem
echo "key-data" > root.pem
echo "DEV_SECRET=xyz" > .env.local
```

**Test dry-run:**
```bash
$IGNLNK lock-all --dry-run
```

**Expected output:**
```
files that would be locked:
  .env
  .env.local
  config/ssl/server.pem
  root.pem
```

Key verification: `*.pem` matches `config/ssl/server.pem` (deep path) — this confirms gitignore slash-less semantics are working correctly.

**Verify:** No files were actually locked (status unchanged).

---

### T14: Lock-All with Patterns

```bash
$IGNLNK lock-all
```

**Expected output:**
```
locked: .env
locked: .env.local
locked: config/ssl/server.pem
locked: root.pem
locked 4 files (3 new, 1 re-locked)
```

**Verify:** `$IGNLNK status` shows all 4 files as locked.

---

### T15: Partial Failure

```bash
echo "test-a" > a.txt
echo "test-b" > b.txt
$IGNLNK lock a.txt b.txt nonexistent.txt
```

**Expected output:**
```
locked: a.txt
locked: b.txt
error: nonexistent.txt: file not found: ...
error: 2 of 3 files locked, 1 failed
```

**Expected exit code:** non-zero

**Verify:**
- `$IGNLNK list` includes both `a.txt` and `b.txt`
- Both have entries in `.ignlnk/manifest.json`
- Successful operations were saved despite the failure

---

### T16: Outside-Project Rejection

```bash
$IGNLNK lock ../outside.txt
```

**Expected output (stderr):**
```
error: ../outside.txt: path ../outside.txt is outside the project root
error: 0 of 1 files locked, 1 failed
```

**Expected exit code:** non-zero

---

### T17: Subdirectory Operation

```bash
cd config/ssl
$IGNLNK status
cd ../..
```

**Expected output:** Same status table as from project root — project root detected by walking up.

---

### T18: Manifest Path Format

```bash
cat .ignlnk/manifest.json
```

**Verify:** All keys use forward slashes regardless of OS (e.g., `config/ssl/server.pem`, never `config\ssl\server.pem`).

---

## Platform-Specific Tests

### Windows Without Developer Mode

> Requires a Windows machine with Developer Mode **disabled**.

**T19: Init warns about symlinks**
```bash
$IGNLNK init
```
**Expected:** Init succeeds but prints warning:
```
warning: symlinks not supported on this system. ignlnk unlock will not work until Developer Mode is enabled (Windows: Settings > Update & Security > For Developers).
```

**T20: Unlock fails with actionable message**
```bash
echo "secret" > test.txt
$IGNLNK lock test.txt
$IGNLNK unlock test.txt
```
**Expected:** Unlock fails with error mentioning Developer Mode.

---

## Stress / Edge Case Tests

> These require manual setup and are harder to automate.

### T21: Concurrent Lock Safety

Open two terminals in the test directory. Run simultaneously:

**Terminal 1:**
```bash
for i in $(seq 1 10); do echo "data-$i" > "file-a-$i.txt"; done
$IGNLNK lock file-a-*.txt
```

**Terminal 2:**
```bash
for i in $(seq 1 10); do echo "data-$i" > "file-b-$i.txt"; done
$IGNLNK lock file-b-*.txt
```

**Verify:** After both complete, `$IGNLNK list` shows all 20 files. No entries lost.

### T22: Large File Warning

```bash
# Create 150MB file
dd if=/dev/zero of=largefile.bin bs=1M count=150 2>/dev/null  # Unix
# or: fsutil file createnew largefile.bin 157286400              # Windows
$IGNLNK lock largefile.bin
```

**Expected:** Warning on stderr: `warning: large file (150 MB): largefile.bin`, but lock succeeds.

### T23: Very Large File Rejection

```bash
# Create 1.1GB file
dd if=/dev/zero of=hugefile.bin bs=1M count=1100 2>/dev/null
$IGNLNK lock hugefile.bin
```

**Expected:** Error: `file exceeds 1GB (1100 MB), use --force to lock large files`

```bash
$IGNLNK lock --force hugefile.bin
```

**Expected:** Lock succeeds with `--force`.

---

## Results Checklist

| # | Test | Pass/Fail | Notes |
|---|------|-----------|-------|
| T01 | Init | | |
| T02 | Init idempotency | | |
| T03 | Lock files | | |
| T04 | Lock idempotency | | |
| T05 | Status (all locked) | | |
| T06 | Unlock | | |
| T07 | Unlock idempotency | | |
| T08 | Status (mixed) | | |
| T09 | Lock-all (re-lock) | | |
| T10 | Forget | | |
| T11 | List | | |
| T12 | Unlock-all | | |
| T13 | .ignlnkfiles dry-run | | |
| T14 | Lock-all with patterns | | |
| T15 | Partial failure | | |
| T16 | Outside-project rejection | | |
| T17 | Subdirectory operation | | |
| T18 | Manifest forward slashes | | |
| T19 | Windows: init symlink warning | | |
| T20 | Windows: unlock fails without Dev Mode | | |
| T21 | Concurrent lock safety | | |
| T22 | Large file warning (>100MB) | | |
| T23 | Very large file rejection (>1GB) | | |

**Environment:**
- OS: _______________
- Go version: _______________
- Symlink support: Yes / No
- Date tested: _______________
- Tested by: _______________
