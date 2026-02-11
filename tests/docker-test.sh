#!/bin/bash
# ignlnk automated test script for Docker
# Runs the scriptable subset of manual-test-procedure.md
# Requires: IGNLNK set to path of ignlnk binary

set -euo pipefail

IGNLNK="${IGNLNK:-/app/ignlnk}"
TEST_DIR="/tmp/ignlnk-test"
FAILED=0

red() { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[33m%s\033[0m\n' "$*"; }

assert() {
    local name="$1"
    local cmd="$2"
    local expect="$3"
    local out
    out=$(eval "$cmd" 2>&1) || true
    if echo "$out" | grep -q -e "$expect"; then
        green "PASS: $name"
    else
        red "FAIL: $name (expected '$expect' in output)"
        echo "  Output: $out"
        FAILED=$((FAILED + 1))
    fi
}

# Safe assert for pre-captured output (avoids eval of content with special chars)
assert_in() {
    local name="$1"
    local output="$2"
    local expect="$3"
    if printf '%s' "$output" | grep -q -e "$expect"; then
        green "PASS: $name"
    else
        red "FAIL: $name (expected '$expect' in output)"
        echo "  Output: $output"
        FAILED=$((FAILED + 1))
    fi
}

assert_not_in() {
    local name="$1"
    local output="$2"
    local reject="$3"
    if printf '%s' "$output" | grep -q -e "$reject"; then
        red "FAIL: $name (expected '$reject' to be absent from output)"
        echo "  Output: $output"
        FAILED=$((FAILED + 1))
    else
        green "PASS: $name"
    fi
}

assert_exit() {
    local name="$1"
    local cmd="$2"
    local want_exit="$3"
    eval "$cmd" 2>/dev/null; local got=$? || true
    # normalize: both 0 or both non-zero
    if [ "$want_exit" -eq 0 ] && [ "$got" -eq 0 ]; then
        green "PASS: $name (exit 0)"
    elif [ "$want_exit" -ne 0 ] && [ "$got" -ne 0 ]; then
        green "PASS: $name (exit non-zero as expected)"
    else
        red "FAIL: $name (expected exit $want_exit, got $got)"
        FAILED=$((FAILED + 1))
    fi
}

assert_exit_nonzero() {
    local name="$1"
    local got="$2"
    if [ "$got" -ne 0 ]; then
        green "PASS: $name (exit $got as expected)"
    else
        red "FAIL: $name (expected non-zero exit, got 0)"
        FAILED=$((FAILED + 1))
    fi
}

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------
yellow "=== ignlnk Docker Test Suite ==="
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Clean any prior vault entry for this dir (in case of rerun in same container)
rm -rf ~/.ignlnk 2>/dev/null || true

# ---------------------------------------------------------------------------
# T01: Init
# ---------------------------------------------------------------------------
yellow "T01: Init"
out=$("$IGNLNK" init 2>&1)
assert_in "T01 init message" "$out" "Initialized ignlnk in"
assert_in "T01 vault line" "$out" "Vault:"
assert "T01 manifest" "cat .ignlnk/manifest.json" '"version"'
assert "T01 vault" "test -d ~/.ignlnk/vault/*/ 2>/dev/null && echo ok" "ok"

# ---------------------------------------------------------------------------
# T02: Init Idempotency
# ---------------------------------------------------------------------------
yellow "T02: Init idempotency"
out=$("$IGNLNK" init 2>&1)
assert_in "T02 warning" "$out" "already initialized"

# ---------------------------------------------------------------------------
# T03: Lock files
# ---------------------------------------------------------------------------
yellow "T03: Lock files"
echo "SECRET=abc123" > .env
echo "password: hunter2" > secrets.yaml
"$IGNLNK" lock .env secrets.yaml
assert "T03 .env placeholder" "cat .env" "\[ignlnk:protected\]"
assert "T03 vault .env" "cat ~/.ignlnk/vault/*/.env" "SECRET=abc123"
assert "T03 vault secrets" "cat ~/.ignlnk/vault/*/secrets.yaml" "hunter2"

# ---------------------------------------------------------------------------
# T04: Lock idempotency
# ---------------------------------------------------------------------------
yellow "T04: Lock idempotency"
out=$("$IGNLNK" lock .env 2>&1)
assert_in "T04" "$out" "already locked"

# ---------------------------------------------------------------------------
# T05: Status
# ---------------------------------------------------------------------------
yellow "T05: Status (all locked)"
out=$("$IGNLNK" status 2>&1)
assert_in "T05" "$out" "locked"

# ---------------------------------------------------------------------------
# T06: Unlock
# ---------------------------------------------------------------------------
yellow "T06: Unlock"
"$IGNLNK" unlock .env
assert "T06 content" "cat .env" "SECRET=abc123"
assert "T06 symlink" "test -L .env && echo symlink" "symlink"

# ---------------------------------------------------------------------------
# T07: Unlock idempotency
# ---------------------------------------------------------------------------
yellow "T07: Unlock idempotency"
out=$("$IGNLNK" unlock .env 2>&1)
assert_in "T07" "$out" "already unlocked"

# ---------------------------------------------------------------------------
# T09: Lock-all (re-lock)
# ---------------------------------------------------------------------------
yellow "T09: Lock-all re-lock"
"$IGNLNK" lock-all
assert "T09 placeholder" "cat .env" "\[ignlnk:protected\]"

# ---------------------------------------------------------------------------
# T10: Forget
# ---------------------------------------------------------------------------
yellow "T10: Forget"
"$IGNLNK" forget secrets.yaml
assert "T10 content" "cat secrets.yaml" "hunter2"
out=$("$IGNLNK" list 2>&1)
assert_in "T10 list contains .env" "$out" ".env"
assert_not_in "T10 secrets.yaml forgotten" "$out" "secrets.yaml"

# ---------------------------------------------------------------------------
# T12: Unlock-all
# ---------------------------------------------------------------------------
yellow "T12: Unlock-all"
"$IGNLNK" unlock-all
assert "T12" "cat .env" "SECRET=abc123"

# ---------------------------------------------------------------------------
# T13: .ignlnkfiles dry-run
# ---------------------------------------------------------------------------
yellow "T13: .ignlnkfiles dry-run"
printf '%s\n' '*.pem' '.env.*' > .ignlnkfiles
mkdir -p config/ssl
echo "cert-data" > config/ssl/server.pem
echo "key-data" > root.pem
echo "DEV_SECRET=xyz" > .env.local
out=$("$IGNLNK" lock-all --dry-run 2>&1)
assert_in "T13 dry-run" "$out" "files that would be locked"
assert_in "T13 config/ssl" "$out" "config/ssl/server.pem"

# ---------------------------------------------------------------------------
# T14: Lock-all with patterns
# ---------------------------------------------------------------------------
yellow "T14: Lock-all with patterns"
"$IGNLNK" lock-all
out=$("$IGNLNK" status 2>&1)
assert_in "T14" "$out" "config/ssl/server.pem"

# ---------------------------------------------------------------------------
# T15: Partial failure
# ---------------------------------------------------------------------------
yellow "T15: Partial failure"
echo "a" > a.txt
echo "b" > b.txt
set +e
"$IGNLNK" lock a.txt b.txt nonexistent.txt >"$TEST_DIR/t15_out.txt" 2>&1
lock_exit=$?
lock_out=$(cat "$TEST_DIR/t15_out.txt")
set -e
assert_exit_nonzero "T15 partial failure exit" "$lock_exit"
assert_in "T15 error mentions nonexistent.txt" "$lock_out" "nonexistent.txt"
assert_in "T15 error message" "$lock_out" "file not found"
out=$("$IGNLNK" list 2>&1)
assert_in "T15 a.txt" "$out" "a.txt"
assert_in "T15 b.txt" "$out" "b.txt"

# ---------------------------------------------------------------------------
# T16: Outside-project rejection
# ---------------------------------------------------------------------------
yellow "T16: Outside-project rejection"
out=$("$IGNLNK" lock ../outside.txt 2>&1) || true
assert_in "T16" "$out" "outside the project root"

# ---------------------------------------------------------------------------
# T17: Subdirectory
# ---------------------------------------------------------------------------
yellow "T17: Subdirectory operation"
out=$(cd config/ssl && "$IGNLNK" status 2>&1)
assert_in "T17" "$out" "locked"
cd /tmp/ignlnk-test

# ---------------------------------------------------------------------------
# T18: Manifest forward slashes
# ---------------------------------------------------------------------------
yellow "T18: Manifest path format"
assert "T18" "cat .ignlnk/manifest.json" 'config/ssl/server.pem'

# ---------------------------------------------------------------------------
# T22: Large file warning (>100MB)
# ---------------------------------------------------------------------------
yellow "T22: Large file warning (150 MB)"
dd if=/dev/zero of=largefile.bin bs=1M count=150 2>/dev/null
out=$("$IGNLNK" lock largefile.bin 2>&1)
assert_in "T22 warning" "$out" "warning: large file"
assert_in "T22 lock succeeds" "$out" "locked: largefile.bin"
"$IGNLNK" unlock largefile.bin
assert "T22 unlock" "test -L largefile.bin && echo ok" "ok"
"$IGNLNK" forget largefile.bin
rm -f largefile.bin

# ---------------------------------------------------------------------------
# T23: Very large file rejection (>1GB)
# ---------------------------------------------------------------------------
yellow "T23: Very large file rejection (1025 MB)"
dd if=/dev/zero of=hugefile.bin bs=1M count=1025 2>/dev/null
out=$("$IGNLNK" lock hugefile.bin 2>&1) || true
assert_in "T23 rejection" "$out" "exceeds 1GB"
assert_in "T23 force hint" "$out" "use --force"
"$IGNLNK" lock --force hugefile.bin 2>&1
assert "T23 force lock" "test -f .ignlnk/manifest.json && grep -q '\"hugefile.bin\"' .ignlnk/manifest.json && echo ok" "ok"
"$IGNLNK" forget hugefile.bin
rm -f hugefile.bin

# ---------------------------------------------------------------------------
# T24: Empty file
# ---------------------------------------------------------------------------
yellow "T24: Empty file"
touch empty.txt
"$IGNLNK" lock empty.txt
"$IGNLNK" unlock empty.txt
assert "T24" "test -z \"$(cat empty.txt)\" && echo empty" "empty"

# ---------------------------------------------------------------------------
# T28: Not a project
# ---------------------------------------------------------------------------
yellow "T28: Not a project"
out=$(cd /tmp && "$IGNLNK" status 2>&1) || true
assert_in "T28" "$out" "not an ignlnk project"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
if [ $FAILED -eq 0 ]; then
    green "=== All tests passed ==="
    exit 0
else
    red "=== $FAILED test(s) failed ==="
    exit 1
fi
