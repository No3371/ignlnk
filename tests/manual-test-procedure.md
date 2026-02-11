# ignlnk Manual Test Procedure

> **Purpose:** Reproducible end-to-end verification of ignlnk CLI across environments.
> **Prerequisites:** Go 1.24+, symlink support (Linux/macOS: native; Windows: Developer Mode enabled)
> **Duration:** ~15–25 minutes (full run including stress tests)
> **Related:** `20260211-ignlnk-mvp-plan.md`, `20260211-ignlnk-mvp-walkthrough.md`

---

## Docker Testing (Automated)

For CI or repeatable runs, use the Docker test image:

```bash
docker build -t ignlnk-test .
docker run --rm ignlnk-test
```

This builds the CLI, runs `go vet` and `go test`, then executes the scriptable subset of this procedure (T01–T18, T22, T23, T24, T28). Large-file tests (T22: 150 MB, T23: 1025 MB) add ~1–2 min runtime. Exit 0 = all passed.

**Interactive shell:**
```bash
docker run --rm -it ignlnk-test /bin/sh
# Then: /app/ignlnk --help
```

---

## Quick Reference

| Phase | Tests | Duration |
|-------|-------|----------|
| Pre-flight | Prerequisites & build | ~1 min |
| Core | T01–T12 | ~5 min |
| Patterns | T13–T14 | ~2 min |
| Error handling | T15–T18 | ~2 min |
| Platform | T19–T20 (Windows only) | ~1 min |
| Stress / edge | T21–T28 | ~5–15 min |

---

## Pre-Flight Checks

Run these **before** any tests. Fail here = do not proceed.

### 1. Go Version

```bash
go version
# Required: go1.24 or later
```

**Verify:** Output shows `go1.24` or higher (e.g. `go1.24.0`, `go1.25`).

### 2. Vet & Tests

```bash
cd <repo-root>
go vet ./...
go test ./...
```

**Verify:** No output from vet; tests pass. Exit code must be 0.

### 3. Build

```bash
go build -o ignlnk .          # Unix / Linux / macOS
go build -o ignlnk.exe .      # Windows
```

**Verify:** Binary exists. Run `./ignlnk` or `ignlnk.exe` — should show usage (no panic).

### 4. Symlink Support (optional but recommended)

**Unix/macOS:** Symlinks are supported by default.

**Windows:**
```powershell
# Check Developer Mode (run in PowerShell)
Get-ItemProperty -Path "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock" -Name "AllowDevelopmentWithoutDevLicense" -ErrorAction SilentlyContinue | Select-Object AllowDevelopmentWithoutDevLicense
# 1 = Developer Mode on (symlinks work)
```

**Fallback:** Locking works without symlinks; unlock will fail. T19–T20 cover this.

### 5. Test Directory

Use a directory **outside** the source repo to avoid `.git` and build artifacts.

```bash
# Unix / Linux / macOS
mkdir -p /tmp/ignlnk-test && cd /tmp/ignlnk-test

# Windows (PowerShell)
New-Item -ItemType Directory -Force -Path $env:TEMP\ignlnk-test | Out-Null; Set-Location $env:TEMP\ignlnk-test
```

---

## Full Clean (Re-Run Preparation)

Run this when re-executing the procedure to ensure a clean slate.

```bash
# Unix / Linux / macOS
TEST_DIR="/tmp/ignlnk-test"
rm -rf "$TEST_DIR"/* "$TEST_DIR"/.ignlnk "$TEST_DIR"/.ignlnkfiles "$TEST_DIR"/.env* 2>/dev/null
cd "$TEST_DIR"

# Windows (PowerShell)
$TEST_DIR = "$env:TEMP\ignlnk-test"
Remove-Item -Recurse -Force "$TEST_DIR\*", "$TEST_DIR\.ignlnk", "$TEST_DIR\.ignlnkfiles", "$TEST_DIR\.env*" -ErrorAction SilentlyContinue
Set-Location $TEST_DIR
```

**Vault cleanup (if test dir was previously used):**
```bash
# Find UID for test dir
cat ~/.ignlnk/index.json   # Unix
Get-Content $env:USERPROFILE\.ignlnk\index.json   # Windows

# Remove matching entry, or for a nuclear reset:
rm -rf ~/.ignlnk   # Unix
Remove-Item -Recurse -Force $env:USERPROFILE\.ignlnk   # Windows
```

---

## Variable Conventions

| Variable | Unix | Windows |
|----------|------|---------|
| Binary | `$IGNLNK` = `./ignlnk` or `/path/to/ignlnk` | `$IGNLNK` = `.\ignlnk.exe` or full path |
| Test dir | `/tmp/ignlnk-test` | `$env:TEMP\ignlnk-test` |
| Home | `~` or `$HOME` | `$env:USERPROFILE` |

**Path output:** Expected outputs below use Unix-style paths. Windows will show backslashes (e.g. `config\ssl\server.pem`). Manifest keys always use forward slashes.

---

## Test Cases

---

### T01: Init

```bash
$IGNLNK init
```

**Expected output (stdout):**
```
Initialized ignlnk in <test-dir>
Vault: <home>/.ignlnk/vault/<uid>
```

**Expected exit code:** 0

**Verify:**
```bash
# Unix
test -f .ignlnk/manifest.json && echo "OK" || echo "FAIL"
grep -q '"version":1' .ignlnk/manifest.json && grep -q '"files":{}' .ignlnk/manifest.json && echo "OK" || echo "FAIL"
test -f ~/.ignlnk/index.json && echo "OK" || echo "FAIL"
test -d ~/.ignlnk/vault/*/ 2>/dev/null && echo "OK" || echo "FAIL"

# Windows (PowerShell)
Test-Path .\.ignlnk\manifest.json
Select-String -Path .\.ignlnk\manifest.json -Pattern '"version":1','"files":\{\}'
Test-Path $env:USERPROFILE\.ignlnk\index.json
(Get-ChildItem $env:USERPROFILE\.ignlnk\vault -Directory).Count -ge 1
```

- `.ignlnk/manifest.json` exists, contains `{"version":1,"files":{}}` (or `"files": {}`)
- `~/.ignlnk/index.json` exists and has an entry for the test directory
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

**Expected exit code:** 0

**Verify:** No duplicate entries in `~/.ignlnk/index.json`; `.ignlnk/` unchanged.

---

### T03: Lock Files

```bash
# Unix
echo "SECRET=abc123" > .env
echo "password: hunter2" > secrets.yaml

# Windows (PowerShell)
Set-Content -Path .env -Value "SECRET=abc123"
Set-Content -Path secrets.yaml -Value "password: hunter2"

$IGNLNK lock .env secrets.yaml
```

**Expected output:**
```
locked: .env
locked: secrets.yaml
```

**Expected exit code:** 0

**Verify:**
```bash
# Placeholder content
cat .env   # Unix
Get-Content .env   # Windows
# Must contain: [ignlnk:protected], "ignlnk unlock .env", "Do NOT attempt"

# Vault copies
# Unix: ls ~/.ignlnk/vault/*/.env ~/.ignlnk/vault/*/secrets.yaml
# Windows: dir $env:USERPROFILE\.ignlnk\vault\*\.env
cat ~/.ignlnk/vault/*/.env   # Unix: should show SECRET=abc123
```

- `cat .env` shows placeholder with `[ignlnk:protected]` and path `.env`
- `cat secrets.yaml` shows equivalent placeholder with path `secrets.yaml`
- Vault copies exist and contain original content
- `.ignlnk/manifest.json` has entries for both with `"state":"locked"` and `"hash":"sha256:..."`

---

### T04: Lock Idempotency

```bash
$IGNLNK lock .env
```

**Expected output:**
```
already locked: .env
```

**Expected exit code:** 0

**Verify:** Manifest and filesystem unchanged (no new writes).

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

**Expected exit code:** 0

---

### T06: Unlock

```bash
$IGNLNK unlock .env
```

**Expected output:**
```
unlocked: .env
```

**Expected exit code:** 0

**Verify:**
```bash
cat .env   # Must show: SECRET=abc123

# Symlink check
ls -la .env   # Unix: shows -> (symlink)
# Windows: (Get-Item .env).Attributes -match "ReparsePoint"
```

- `cat .env` shows `SECRET=abc123`
- File is a symlink pointing to `~/.ignlnk/vault/<uid>/.env`
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

**Expected exit code:** 0

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

**Expected exit code:** 0

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

**Expected exit code:** 0

**Verify:**
- `cat .env` shows placeholder again (not symlink)
- `$IGNLNK status` shows both `.env` and `secrets.yaml` locked

---

### T10: Forget

```bash
$IGNLNK forget secrets.yaml
```

**Expected output:**
```
forgot: secrets.yaml (restored to original location)
```

**Expected exit code:** 0

**Verify:**
- `cat secrets.yaml` shows `password: hunter2`
- `secrets.yaml` is regular file (not symlink or placeholder)
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

**Expected exit code:** 0

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

**Expected exit code:** 0

**Verify:** `cat .env` shows `SECRET=abc123`.

---

### T13: .ignlnkfiles Pattern Matching

**Setup:**
```bash
# Unix
cat > .ignlnkfiles << 'EOF'
*.pem
.env.*
EOF
mkdir -p config/ssl
echo "cert-data" > config/ssl/server.pem
echo "key-data" > root.pem
echo "DEV_SECRET=xyz" > .env.local

# Windows (PowerShell)
@"
*.pem
.env.*
"@ | Set-Content -Path .ignlnkfiles
New-Item -ItemType Directory -Force -Path config\ssl | Out-Null
Set-Content -Path config\ssl\server.pem -Value "cert-data"
Set-Content -Path root.pem -Value "key-data"
Set-Content -Path .env.local -Value "DEV_SECRET=xyz"
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

(Windows may show `config\ssl\server.pem`.)

**Key check:** `*.pem` matches `config/ssl/server.pem` — gitignore-style slash-less semantics.

**Verify:** No files actually locked; status unchanged.

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

**Expected exit code:** 0

**Verify:** `$IGNLNK status` shows all 4 files locked.

---

### T15: Partial Failure

```bash
# Unix
echo "test-a" > a.txt
echo "test-b" > b.txt

# Windows
Set-Content -Path a.txt -Value "test-a"
Set-Content -Path b.txt -Value "test-b"

$IGNLNK lock a.txt b.txt nonexistent.txt
echo "Exit: $?"   # Unix
$LASTEXITCODE    # Windows PowerShell
```

**Expected output:**
```
locked: a.txt
locked: b.txt
error: nonexistent.txt: file not found: ...
```

**Expected exit code:** non-zero

**Verify:**
- `$IGNLNK list` includes `a.txt` and `b.txt`
- Both in `.ignlnk/manifest.json`
- Successful operations persisted despite failure

---

### T16: Outside-Project Rejection

```bash
$IGNLNK lock ../outside.txt
```

**Expected output (stderr):**
```
error: ../outside.txt: path <abs-path> is outside the project root
error: 0 of 1 files locked, 1 failed
```

**Expected exit code:** non-zero

---

### T17: Subdirectory Operation

```bash
cd config/ssl
$IGNLNK status
cd ../..   # or cd ..\.. on Windows
```

**Expected:** Same status as from project root. Project root found by walking up.

---

### T18: Manifest Path Format

```bash
cat .ignlnk/manifest.json   # Unix
Get-Content .ignlnk\manifest.json   # Windows
```

**Verify:** All keys use forward slashes (e.g. `config/ssl/server.pem`), never backslashes.

---

## Platform-Specific Tests

### Windows Without Developer Mode

Requires Developer Mode **disabled**.

**T19: Init warns about symlinks**
```bash
$IGNLNK init
```
**Expected:** Init succeeds; stderr shows:
```
warning: symlinks not supported on this system. ignlnk unlock will not work until Developer Mode is enabled (Windows: Settings > Update & Security > For Developers).
```

**T20: Unlock fails with actionable message**
```bash
echo "secret" > test.txt
$IGNLNK lock test.txt
$IGNLNK unlock test.txt
```
**Expected:** Unlock fails with error about symlinks/Developer Mode.

---

## Stress / Edge Case Tests

### T21: Concurrent Lock Safety

Two terminals, same test directory. Run in parallel:

**Terminal 1:**
```bash
# Unix
for i in $(seq 1 10); do echo "data-$i" > "file-a-$i.txt"; done
$IGNLNK lock file-a-*.txt

# Windows
1..10 | ForEach-Object { Set-Content -Path "file-a-$_.txt" -Value "data-$_" }
$IGNLNK lock file-a-*.txt
```

**Terminal 2:**
```bash
# Unix
for i in $(seq 1 10); do echo "data-$i" > "file-b-$i.txt"; done
$IGNLNK lock file-b-*.txt

# Windows
1..10 | ForEach-Object { Set-Content -Path "file-b-$_.txt" -Value "data-$_" }
$IGNLNK lock file-b-*.txt
```

**Verify:** `$IGNLNK list` shows all 20 files; no lost entries; no corruption.

---

### T22: Large File Warning

```bash
# Unix (150 MB)
dd if=/dev/zero of=largefile.bin bs=1M count=150 2>/dev/null

# Windows (PowerShell, 150 MB)
$bytes = New-Object byte[] (150 * 1024 * 1024)
[IO.File]::WriteAllBytes("$PWD\largefile.bin", $bytes)

$IGNLNK lock largefile.bin
```

**Expected:** Warning on stderr: `warning: large file (150 MB): largefile.bin`; lock succeeds.

---

### T23: Very Large File Rejection

```bash
# Unix (1.1 GB)
dd if=/dev/zero of=hugefile.bin bs=1M count=1100 2>/dev/null

# Windows (1.1 GB) — requires ~1.1 GB free space
# fsutil file createnew hugefile.bin 1153433600
```

```bash
$IGNLNK lock hugefile.bin
```

**Expected:** Error like `file exceeds 1GB (1100 MB), use --force to lock large files`; non-zero exit.

```bash
$IGNLNK lock --force hugefile.bin
```

**Expected:** Lock succeeds with `--force`.

---

### T24: Empty File

```bash
touch empty.txt
$IGNLNK lock empty.txt
$IGNLNK unlock empty.txt
cat empty.txt
```

**Expected:** Lock/unlock succeed; `cat empty.txt` is empty (no crash, no corruption).

---

### T25: Special Characters in Filename

```bash
# Unix — avoid if filesystem is problematic
touch "file with spaces.txt"
echo "data" >> "file with spaces.txt"
$IGNLNK lock "file with spaces.txt"
$IGNLNK unlock "file with spaces.txt"
cat "file with spaces.txt"
```

**Expected:** Lock/unlock succeed; content preserved.

---

### T26: No .ignlnkfiles (Lock-All Idempotency)

From project root with no `.ignlnkfiles`:

```bash
rm -f .ignlnkfiles   # Unix
Remove-Item .ignlnkfiles -ErrorAction SilentlyContinue   # Windows
$IGNLNK lock-all
```

**Expected:** `nothing to lock` (only manages already-tracked files). No error.

---

### T27: Vault Mirror Backup

After locking any file:

```bash
# Unix
ls -la ~/.ignlnk/vault/
# Expect: <uid>/ and <uid>.backup/ directories
diff ~/.ignlnk/vault/<uid>/.env ~/.ignlnk/vault/<uid>.backup/.env   # should match

# Windows
Get-ChildItem $env:USERPROFILE\.ignlnk\vault\
# Expect: <uid> and <uid>.backup
```

**Verify:** `.backup` mirror exists and matches vault content for locked files.

---

### T28: Not a Project (No Init)

```bash
cd /tmp   # or any dir without .ignlnk
$IGNLNK status
```

**Expected:** Error like `not an ignlnk project (no .ignlnk/ found in ...)`; non-zero exit.

---

## Recovery Procedures

### Stale manifest.lock

If a previous run was killed:
```bash
rm .ignlnk/manifest.lock   # Unix
Remove-Item .ignlnk\manifest.lock -Force   # Windows
```

### Corrupt manifest.json

Restore from backup if available; otherwise re-init and re-add files. Vault data may still be usable.

### Wrong Unlock (Modified Placeholder)

If a placeholder was edited:
- Unlock may fail (hash mismatch); stderr shows `warning: vault file hash mismatch`
- Restore from vault or `<uid>.backup` manually if needed

---

## Troubleshooting

| Symptom | Check |
|---------|-------|
| `not an ignlnk project` | Run from project root or subdir; ensure `.ignlnk/` exists |
| `could not acquire lock` | Another ignlnk running; or remove `manifest.lock` |
| Unlock fails on Windows | Developer Mode or run as Administrator |
| `path ... is outside the project root` | Do not use `../` or absolute paths outside project |
| `file not found` | File must exist and be a regular file before lock |
| Hash mismatch | Vault or placeholder was modified; restore from backup |

---

## Results Checklist

| # | Test | Pass | Fail | Notes |
|---|------|------|------|-------|
| T01 | Init | ☐ | ☐ | |
| T02 | Init idempotency | ☐ | ☐ | |
| T03 | Lock files | ☐ | ☐ | |
| T04 | Lock idempotency | ☐ | ☐ | |
| T05 | Status (all locked) | ☐ | ☐ | |
| T06 | Unlock | ☐ | ☐ | |
| T07 | Unlock idempotency | ☐ | ☐ | |
| T08 | Status (mixed) | ☐ | ☐ | |
| T09 | Lock-all (re-lock) | ☐ | ☐ | |
| T10 | Forget | ☐ | ☐ | |
| T11 | List | ☐ | ☐ | |
| T12 | Unlock-all | ☐ | ☐ | |
| T13 | .ignlnkfiles dry-run | ☐ | ☐ | |
| T14 | Lock-all with patterns | ☐ | ☐ | |
| T15 | Partial failure | ☐ | ☐ | |
| T16 | Outside-project rejection | ☐ | ☐ | |
| T17 | Subdirectory operation | ☐ | ☐ | |
| T18 | Manifest forward slashes | ☐ | ☐ | |
| T19 | Windows: init symlink warning | ☐ | ☐ | N/A on Unix |
| T20 | Windows: unlock fails without Dev Mode | ☐ | ☐ | N/A on Unix |
| T21 | Concurrent lock safety | ☐ | ☐ | |
| T22 | Large file warning (>100MB) | ☐ | ☐ | |
| T23 | Very large file rejection (>1GB) | ☐ | ☐ | |
| T24 | Empty file | ☐ | ☐ | |
| T25 | Special chars in filename | ☐ | ☐ | |
| T26 | No .ignlnkfiles lock-all | ☐ | ☐ | |
| T27 | Vault mirror backup | ☐ | ☐ | |
| T28 | Not a project | ☐ | ☐ | |

**Environment:**
- OS: _______________
- Go version: _______________
- Symlink support: Yes / No / N/A
- Date: _______________
- Tester: _______________
