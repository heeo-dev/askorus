#!/usr/bin/env bash
# The agent's background commands for real on macOS and Linux (#52): start,
# status, shutdown, path and autostart, with the program built here. The Go
# tests cover the pieces; this runs the commands as a person does, on the
# real system. It is what stands in for a Mac on the developer's PC: the
# workflow runs it on GitHub's macOS and Linux machines.
#
# The agent's folder, the home folder (start files, the launch agent's
# file) and the cache folder (lock files) are inside one temp folder, and
# the agent gets a port of its own: a real agent on this machine is never
# touched. Two parts ask the real system and are off unless asked for:
#   ASKORUS_TEST_LAUNCHD=1   macOS: load the launch agent's file into
#                            launchd and see the agent come up
#   ASKORUS_TEST_SYSTEMD=1   Linux: turn the systemd user unit on in the
#                            real home folder, start it, and remove it
# Exit code 0 only if all pass.
#   bash scripts/test-unix.sh [-keep]
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd -P)"
work="$(mktemp -d "${TMPDIR:-/tmp}/askorus-unix-test-XXXXXXXX")"
work="$(cd "$work" && pwd -P)"
keep=0
[ "${1:-}" = -keep ] && keep=1
fails=0
port=$((46000 + $$ % 900))
real_home="$HOME"

check() { # check <name> <0 when ok> [detail]
  if [ "$2" = 0 ]; then
    printf '   ok    %s\n' "$1"
  else
    printf '   FAIL  %s\n' "$1"
    [ -n "${3:-}" ] && printf '%s\n' "$3" | sed 's/^/         /' | head -n 20
    fails=$((fails + 1))
  fi
}
note() { printf '   note  %s\n' "$1"; }
has() { printf '%s' "$1" | grep -qE -- "$2"; }

case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
exe="$work/bin/askorus-agent"
mkdir -p "$work/bin"
echo "== build ($os, $(uname -m))"
(cd "$root/agent" && CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=0.0.0-test" -o "$exe" ./cmd/askorus-agent)
check 'the agent builds' $? ''
[ -x "$exe" ] || { echo 'FAILED: no agent to run'; exit 1; }

export HOME="$work/home" XDG_CACHE_HOME="$work/home/.cache" XDG_CONFIG_HOME="$work/home/.config"
export ASKORUS_AGENT_DIR="$work/agent"
mkdir -p "$HOME" "$ASKORUS_AGENT_DIR"
printf '{"port": %d}\n' "$port" > "$ASKORUS_AGENT_DIR/agent.json"
listening() { curl -s -o /dev/null -m 3 -w '%{http_code}' "http://127.0.0.1:$port/v1/info" 2>/dev/null | grep -q '^401$'; }

echo '== start, status, shutdown'
out="$("$exe" status 2>&1)"; has "$out" '^not running'; check 'status: not running at first' $? "$out"
out="$("$exe" shutdown 2>&1)"; has "$out" 'is not running'; check 'shutdown with nothing running says so' $? "$out"
out="$("$exe" start 2>&1)"; code=$?
[ "$code" = 0 ] && has "$out" "runs in the background on 127\.0\.0\.1:$port"; check 'start puts it in the background' $? "$out"
listening; check 'it answers on its port (401 without a token)' $?
out="$("$exe" status 2>&1)"; has "$out" "^running on 127\.0\.0\.1:$port"; check 'status: running' $? "$out"
out="$("$exe" start 2>&1)"; has "$out" 'already runs'; check 'a second start changes nothing' $? "$out"
[ -s "$ASKORUS_AGENT_DIR/logs/agent.log" ] && [ -e "$ASKORUS_AGENT_DIR/logs/background.out" ]; check 'it writes its log files' $?
pid="$(cat "$ASKORUS_AGENT_DIR/agent.pid" 2>/dev/null || echo 0)"
kill -0 "$pid" 2>/dev/null; check 'its process id is on file and alive' $?
out="$("$exe" shutdown 2>&1)"; code=$?
[ "$code" = 0 ] && has "$out" 'the agent stopped'; check 'shutdown stops it' $? "$out"
! listening && ! kill -0 "$pid" 2>/dev/null; check 'the port is closed and the process is gone' $?
out="$("$exe" status 2>&1)"; has "$out" '^not running'; check 'status: not running again' $? "$out"

echo '== serve in the foreground ends on the stop signal'
"$exe" serve > "$work/serve.out" 2>&1 &
spid=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do listening && break; sleep 0.5; done
listening; check 'serve listens' $? "$(cat "$work/serve.out")"
"$exe" shutdown > /dev/null 2>&1
wait "$spid"; check 'serve ended cleanly (exit code 0)' $? "$(tail -n 5 "$work/serve.out")"

echo '== path'
export SHELL=/bin/zsh
bindir="$(dirname "$exe")"
out="$("$exe" path status 2>&1)"; has "$out" '^off'; check 'path status: off' $? "$out"
out="$("$exe" path on 2>&1)"; has "$out" 'added .* to this user'"'"'s PATH'; check 'path on adds the folder' $? "$out"
grep -q "export PATH=\"\$PATH\":\"$bindir\"" "$HOME/.zshrc" 2>/dev/null; check 'the line is in the start file of the shell in use' $? "$(cat "$HOME/.zshrc" 2>&1)"
[ "$(zsh -c 'source ~/.zshrc; command -v askorus-agent' 2>/dev/null || sh -c ". '$HOME/.zshrc'; command -v askorus-agent")" = "$exe" ]; check 'a new shell finds askorus-agent' $?
out="$("$exe" path status 2>&1)"; has "$out" '^on: '; check 'path status: on' $? "$out"
out="$("$exe" path off 2>&1)"; has "$out" 'removed '; check 'path off removes it' $? "$out"
[ ! -s "$HOME/.zshrc" ]; check 'the start file is as it was' $? "$(cat "$HOME/.zshrc" 2>&1)"

echo '== autostart'
if [ "$os" = darwin ]; then
  plist="$HOME/Library/LaunchAgents/app.askorus.agent.plist"
  out="$("$exe" autostart status 2>&1)"; has "$out" '^off'; check 'autostart status: off' $? "$out"
  out="$("$exe" autostart on 2>&1)"; code=$?
  [ "$code" = 0 ] && [ -f "$plist" ]; check 'autostart on writes the launch agent' $? "$out"
  plutil -lint "$plist" > "$work/plutil.out" 2>&1; check 'macOS reads the file as a property list' $? "$(cat "$work/plutil.out")"
  out="$("$exe" autostart status 2>&1)"; has "$out" "^on: \"$exe\" serve"; check 'autostart status: on, with the command' $? "$out"
  out="$("$exe" status 2>&1)"; has "$out" 'starts at logon'; check 'status says it starts at logon' $? "$out"
  if [ "${ASKORUS_TEST_LAUNCHD:-}" = 1 ]; then
    # launchd itself: load the file and see the agent come up as it would at logon.
    domain="gui/$(id -u)"
    launchctl bootstrap "$domain" "$plist" > "$work/launchctl.out" 2>&1; bcode=$?
    if [ "$bcode" != 0 ]; then
      domain="user/$(id -u)"
      launchctl bootstrap "$domain" "$plist" >> "$work/launchctl.out" 2>&1; bcode=$?
    fi
    check "launchd takes the file ($domain)" "$bcode" "$(cat "$work/launchctl.out")"
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do listening && break; sleep 0.5; done
    listening; check 'the agent comes up under launchd' $? "$(launchctl print "$domain/app.askorus.agent" 2>&1 | head -n 30; cat "$XDG_CACHE_HOME/../Library/Caches/Askorus/background.out" 2>/dev/null | tail -n 5)"
    out="$("$exe" shutdown 2>&1)"; has "$out" 'the agent stopped'; check 'shutdown stops the agent launchd started' $? "$out"
    sleep 1
    ! listening; check 'launchd does not start it again' $?
    launchctl bootout "$domain/app.askorus.agent" > /dev/null 2>&1
  else
    note 'launchd itself was not asked (ASKORUS_TEST_LAUNCHD=1 does)'
  fi
  out="$("$exe" autostart off 2>&1)"; [ ! -e "$plist" ] && has "$out" 'no longer starts'; check 'autostart off removes the file' $? "$out"
elif [ "${ASKORUS_TEST_SYSTEMD:-}" = 1 ]; then
  # The real systemd of this user, so the real home folder: the unit is removed again.
  if HOME="$real_home" XDG_CONFIG_HOME= systemctl --user show-environment > /dev/null 2>&1; then
    # systemd gives the agent this user's real folders, so the commands that talk to it use them too.
    sys() { HOME="$real_home" XDG_CONFIG_HOME= XDG_CACHE_HOME= "$@"; }
    unit="$real_home/.config/systemd/user/askorus-agent.service"
    if [ -e "$unit" ]; then
      note 'this user already has the unit: left alone, the systemd part is skipped'
    else
      out="$(sys "$exe" autostart on 2>&1)"; code=$?
      [ "$code" = 0 ] && [ -f "$unit" ]; check 'autostart on writes and enables the unit' $? "$out"
      [ "$(sys systemctl --user is-enabled askorus-agent.service 2>&1)" = enabled ]; check 'systemd says enabled' $?
      out="$(sys "$exe" autostart status 2>&1)"; has "$out" "^on: \"$exe\" serve"; check 'autostart status: on, with the command' $? "$out"
      sys systemctl --user start askorus-agent.service > /dev/null 2>&1
      for _ in 1 2 3 4 5 6 7 8 9 10; do listening && break; sleep 0.5; done
      listening; check 'the agent comes up under systemd' $? "$(sys systemctl --user status askorus-agent.service 2>&1 | head -n 15)"
      out="$(sys "$exe" shutdown 2>&1)"; has "$out" 'the agent stopped'; check 'shutdown stops the agent systemd started' $? "$out"
      sleep 1
      [ "$(sys systemctl --user is-active askorus-agent.service 2>&1)" != active ]; check 'systemd does not start it again' $?
      out="$(sys "$exe" autostart off 2>&1)"; [ ! -e "$unit" ] && has "$out" 'no longer starts'; check 'autostart off removes the unit' $? "$out"
      sys systemctl --user stop askorus-agent.service > /dev/null 2>&1 # whatever happened above, nothing of the test stays
    fi
  else
    note 'this user'"'"'s systemd does not answer here: the systemd part is skipped'
    out="$("$exe" autostart on 2>&1)"; code=$?
    [ "$code" != 0 ] && has "$out" 'systemd does not answer'; check 'autostart on says why it cannot be turned on' $? "$out"
  fi
else
  note 'systemd itself was not asked (ASKORUS_TEST_SYSTEMD=1 does)'
  out="$("$exe" autostart status 2>&1)"; has "$out" '^off'; check 'autostart status: off' $? "$out"
fi

echo '== check runs and names this system'"'"'s way to start'
out="$("$exe" check 2>&1)"
has "$out" 'normal user rights' && has "$out" 'askorus-agent start'; check 'check lists what is missing, with start' $? "$(printf '%s' "$out" | head -n 12)"
! has "$out" 'only supported on Windows'; check 'nothing says Windows only' $? "$out"

"$exe" shutdown > /dev/null 2>&1
if [ "$keep" = 1 ]; then echo "kept: $work"; else rm -rf "$work"; fi
if [ "$fails" = 0 ]; then echo 'ALL PASSED'; else echo "FAILED: $fails"; exit 1; fi
