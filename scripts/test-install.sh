#!/usr/bin/env bash
# Tests scripts/install.sh on macOS and Linux (#52), as test-install.ps1
# tests install.ps1: a new install, one through a pipe (as `curl | sh` runs
# it), a damaged download, a release without this system's file, a refused
# address, an update while the agent runs, one while it does not, and one
# of an agent that does not stop (R-178).
# Nothing of a real install is touched: the install folder, the agent
# folder (ASKORUS_AGENT_DIR, with a port of its own), the home folder and
# the temporary folder are all inside one temp folder, and setup is not run
# (ASKORUS_NO_SETUP). Exit code 0 only if all pass.
#   bash scripts/test-install.sh [-keep]
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd -P)"
work="$(mktemp -d "${TMPDIR:-/tmp}/askorus-install-test-XXXXXXXX")"
work="$(cd "$work" && pwd -P)"
keep=0
[ "${1:-}" = -keep ] && keep=1
fails=0
port=$((47000 + $$ % 900))

check() { # check <name> <0 when ok> [detail]
  if [ "$2" = 0 ]; then
    printf '   ok    %s\n' "$1"
  else
    printf '   FAIL  %s\n' "$1"
    [ -n "${3:-}" ] && printf '%s\n' "$3" | sed 's/^/         /' | head -n 20
    fails=$((fails + 1))
  fi
}
# No pipe: with pipefail, a grep -q that has seen enough ends the writer with a broken pipe
# and the whole test reads as failed (seen on GitHub's Linux runner, never on the PC).
has() { grep -qE -- "$2" <<< "$1"; }
sum() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi; }

case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
case "$(uname -m)" in arm64 | aarch64) arch=arm64 ;; *) arch=amd64 ;; esac
asset="askorus-agent-$os-$arch"

# Two releases, built here as release.ps1 builds them.
build() { # build <version> <folder>
  mkdir -p "$2"
  (cd "$root/agent" && CGO_ENABLED=0 go build -trimpath -tags ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail -ldflags "-s -w -X main.version=$1" -o "$2/$asset" ./cmd/askorus-agent) || return 1
  cp "$root/scripts/install.sh" "$2/install.sh"
  (cd "$2" && for f in "$asset" install.sh; do printf '%s  %s\n' "$(sum "$f")" "$f"; done > SHA256SUMS.txt)
}
echo '== release files'
build 0.0.1-test "$work/rel1" && build 0.0.2-test "$work/rel2"
check 'two releases are built' $? ''
[ -x "$work/rel1/$asset" ] || { echo 'FAILED: no agent to install'; exit 1; }

# From here on the agent and the installer see only the temp folder.
real_path="$PATH"
export HOME="$work/home" XDG_CACHE_HOME="$work/home/.cache" XDG_CONFIG_HOME="$work/home/.config" TMPDIR="$work/tmp"
export ASKORUS_AGENT_DIR="$work/agent" ASKORUS_NO_SETUP=1
# The test agent runs without its Tailscale node (#57): it signs in nowhere
# and talks to no server.
export ASKORUS_TEST_NO_TAILNET=1
mkdir -p "$HOME" "$TMPDIR" "$ASKORUS_AGENT_DIR"
printf '{"port": %d}\n' "$port" > "$ASKORUS_AGENT_DIR/agent.json"
unset ASKORUS_VERSION ASKORUS_INSTALL_DIR ASKORUS_DOWNLOAD_BASE

install() { # install <base> <dir>: runs the script as a file; sets $out and $code
  out="$(ASKORUS_DOWNLOAD_BASE="$1" ASKORUS_INSTALL_DIR="$2" sh "$root/scripts/install.sh" 2>&1)"
  code=$?
}
left() { find "$TMPDIR" -maxdepth 1 -name 'askorus-install-????????' | wc -l | tr -d ' '; }
running() { case "$("$1" status 2>/dev/null)" in running*) return 0 ;; esac; return 1; }

dir="$work/bin"
exe="$dir/askorus-agent"

echo '== a new install'
install "$work/rel1" "$dir"
check 'exit code 0' "$code" "$out"
[ -x "$exe" ]; check 'the agent is in the install folder' $? "$out"
[ ! -e "$dir/askorus-agent.new" ]; check 'no askorus-agent.new is left' $?
[ "$("$exe" version 2>/dev/null)" = 'askorus-agent 0.0.1-test' ]; check 'it is the released version' $? "$out"
has "$out" 'the SHA-256 matches' && has "$out" 'installed askorus-agent 0\.0\.1-test' && has "$out" 'setup'; check 'it says what it did and what is next' $? "$out"
[ "$(left)" = 0 ]; check 'no temp folder is left' $?

echo '== through a pipe, as curl | sh runs it'
out="$(ASKORUS_DOWNLOAD_BASE="$work/rel1" ASKORUS_INSTALL_DIR="$work/piped/bin" sh < "$root/scripts/install.sh" 2>&1)"; code=$?
check 'exit code 0' "$code" "$out"
[ "$("$work/piped/bin/askorus-agent" version 2>/dev/null)" = 'askorus-agent 0.0.1-test' ]; check 'installed, in a folder it made' $? "$out"

echo '== a damaged download'
cp -R "$work/rel1" "$work/bad"
printf 'x' >> "$work/bad/$asset"
install "$work/bad" "$work/baddir"
[ "$code" = 1 ]; check 'exit code 1' $? "$out"
has "$out" 'damaged' && has "$out" 'nothing was installed'; check 'says the download is damaged and nothing was installed' $? "$out"
[ ! -e "$work/baddir/askorus-agent" ] && [ ! -e "$work/baddir/askorus-agent.new" ]; check 'nothing is installed' $?
[ "$(left)" = 0 ]; check 'no temp folder is left' $?

echo '== a release without this system'"'"'s file'
mkdir -p "$work/other"
cp "$work/rel1/$asset" "$work/other/$asset"
printf '%s  %s\n' "$(sum "$work/rel1/$asset")" 'askorus-agent-windows-amd64.exe' > "$work/other/SHA256SUMS.txt"
install "$work/other" "$work/otherdir"
[ "$code" = 1 ] && has "$out" "no line for $asset" && [ ! -e "$work/otherdir/askorus-agent" ]; check 'refused: the sums do not list it' $? "$out"
install "$work/nowhere" "$work/otherdir"
[ "$code" = 1 ] && has "$out" 'has no file'; check 'a folder without the files: says so' $? "$out"

echo '== an address that is not https'
install 'http://example.invalid/releases' "$work/httpdir"
[ "$code" = 1 ] && has "$out" 'https address or a folder'; check 'refused' $? "$out"

echo '== an update while the agent runs'
out="$("$exe" start 2>&1)"
running "$exe"; check 'the test agent runs' $? "$out"
install "$work/rel2" "$dir"
check 'exit code 0' "$code" "$out"
has "$out" 'stopping the agent' && has "$out" 'updated askorus-agent 0\.0\.1-test to askorus-agent 0\.0\.2-test'; check 'stopped it, updated, kept the old one' $? "$out"
[ "$("$exe" version 2>/dev/null)" = 'askorus-agent 0.0.2-test' ]; check 'the new agent is in place' $?
[ "$("$dir/askorus-agent.prev" version 2>/dev/null)" = 'askorus-agent 0.0.1-test' ]; check 'the old one is askorus-agent.prev' $?
running "$exe"; check 'the agent runs again' $? "$out"
"$exe" shutdown > /dev/null 2>&1
! running "$exe"; check 'and stops when asked' $?

echo '== an update while it does not run'
install "$work/rel1" "$dir"
[ "$code" = 0 ] && ! has "$out" 'stopping'; check 'exit code 0, no stop' $? "$out"
[ "$("$exe" version 2>/dev/null)" = 'askorus-agent 0.0.1-test' ]; check 'the other version is in place' $?
! running "$exe"; check 'does not start it' $?
[ "$(left)" = 0 ]; check 'no temp folder is left' $?

echo '== an update of an agent that does not stop (R-178)'
# A second agent, installed elsewhere, which the update must leave alone.
mkdir -p "$work/second/bin" "$work/second/agent"
cp "$work/rel1/$asset" "$work/second/bin/askorus-agent"
chmod 755 "$work/second/bin/askorus-agent"
printf '{"port": %d}\n' "$((port + 1000))" > "$work/second/agent/agent.json"
second() { ASKORUS_AGENT_DIR="$work/second/agent" "$work/second/bin/askorus-agent" "$@"; }
second start > /dev/null 2>&1
# And a third that runs from the very same program, with settings and a port
# of its own (R-179: that one was ended too).
mkdir -p "$work/third"
printf '{"port": %d}\n' "$((port + 2000))" > "$work/third/agent.json"
ASKORUS_AGENT_DIR="$work/third" "$exe" start > /dev/null 2>&1
third_pid="$(cat "$work/third/agent.pid" 2>/dev/null)"
"$exe" start > /dev/null 2>&1
running "$exe"; check 'the test agent runs' $? ''
pid="$(cat "$ASKORUS_AGENT_DIR/agent.pid" 2>/dev/null)"
second_pid="$(cat "$work/second/agent/agent.pid" 2>/dev/null)"
if [ -n "$pid" ] && kill -STOP "$pid"; then
  install "$work/rel2" "$dir" # about 30 seconds: shutdown waits that long first
  check 'exit code 0' "$code" "$out"
  has "$out" 'still running after 30 s' && has "$out" 'ending it'; check 'it did not stop, and was ended' $? "$out"
  [ "$("$exe" version 2>/dev/null)" = 'askorus-agent 0.0.2-test' ]; check 'the new agent is in place' $? "$out"
  running "$exe" && [ "$(cat "$ASKORUS_AGENT_DIR/agent.pid" 2>/dev/null)" != "$pid" ]; check 'a new agent runs' $? "$out"
  ! kill -0 "$pid" 2>/dev/null; check 'the stuck one is gone' $?
  [ -n "$second_pid" ] && kill -0 "$second_pid" 2>/dev/null; check 'the agent installed elsewhere still runs' $?
  [ -n "$third_pid" ] && kill -0 "$third_pid" 2>/dev/null; check 'the agent of other settings from the same program still runs' $?
  kill -CONT "$pid" 2>/dev/null
else
  check 'the test agent can be paused' 1
fi
"$exe" shutdown > /dev/null 2>&1
second shutdown > /dev/null 2>&1
ASKORUS_AGENT_DIR="$work/third" "$exe" shutdown > /dev/null 2>&1

export PATH="$real_path"
if [ "$keep" = 1 ]; then echo "kept: $work"; else rm -rf "$work"; fi
if [ "$fails" = 0 ]; then echo 'ALL PASSED'; else echo "FAILED: $fails"; exit 1; fi
