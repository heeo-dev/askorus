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
#                            launchd, see the agent come up, and update it
#                            as the phone's button does (#71)
#   ASKORUS_TEST_SYSTEMD=1   Linux: turn the systemd user unit on in the
#                            real home folder, start it, update it as the
#                            phone's button does (#71), and remove it
# The update's test builds lay scripts/testdata/update-seams.go.txt over the
# agent (a throwaway signing key, releases from a folder). One of its cases
# waits the minute an old agent is given to stop.
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
# No pipe: with pipefail, a grep -q that has seen enough ends the writer with a broken pipe
# and the whole test reads as failed (seen on GitHub's Linux runner, never on the PC).
has() { grep -qE -- "$2" <<< "$1"; }

case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
exe="$work/bin/askorus-agent"
mkdir -p "$work/bin"
echo "== build ($os, $(uname -m))"
(cd "$root/agent" && CGO_ENABLED=0 go build -trimpath -tags ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail -ldflags "-X main.version=0.0.0-test" -o "$exe" ./cmd/askorus-agent)
check 'the agent builds' $? ''
[ -x "$exe" ] || { echo 'FAILED: no agent to run'; exit 1; }

export HOME="$work/home" XDG_CACHE_HOME="$work/home/.cache" XDG_CONFIG_HOME="$work/home/.config"
export ASKORUS_AGENT_DIR="$work/agent"
mkdir -p "$HOME" "$ASKORUS_AGENT_DIR"
printf '{"port": %d}\n' "$port" > "$ASKORUS_AGENT_DIR/agent.json"
# The test agent runs without its Tailscale node: it signs in nowhere, talks
# to no server and no phone can reach it (#57). It opens no port on this
# computer either, so whether it runs is what its own status says.
export ASKORUS_TEST_NO_TAILNET=1
# No pipe here either: status goes on to ask the system about the logon start.
uexe="$exe" # the agent of the update's test, once that has one
running() { case "$("$uexe" status 2>/dev/null)" in running*) return 0 ;; esac; return 1; }

echo '== start, status, shutdown'
out="$("$exe" status 2>&1)"; has "$out" '^not running'; check 'status: not running at first' $? "$out"
out="$("$exe" shutdown 2>&1)"; has "$out" 'is not running'; check 'shutdown with nothing running says so' $? "$out"
out="$("$exe" start 2>&1)"; code=$?
[ "$code" = 0 ] && has "$out" 'runs in the background \(pid [0-9]+\)'; check 'start puts it in the background' $? "$out"
has "$out" 'runs without Tailscale \(a test run'; check 'start says it is a run without Tailscale' $? "$out"
out="$("$exe" status 2>&1)"; has "$out" '^running$'; check 'status: running' $? "$out"
out="$("$exe" start 2>&1)"; has "$out" 'already runs'; check 'a second start changes nothing' $? "$out"
[ -s "$ASKORUS_AGENT_DIR/logs/agent.log" ] && [ -e "$ASKORUS_AGENT_DIR/logs/background.out" ]; check 'it writes its log files' $?
pid="$(cat "$ASKORUS_AGENT_DIR/agent.pid" 2>/dev/null || echo 0)"
kill -0 "$pid" 2>/dev/null; check 'its process id is on file and alive' $?
out="$("$exe" shutdown 2>&1)"; code=$?
[ "$code" = 0 ] && has "$out" 'the agent stopped'; check 'shutdown stops it' $? "$out"
! running && ! kill -0 "$pid" 2>/dev/null; check 'it runs no more and the process is gone' $?
out="$("$exe" status 2>&1)"; has "$out" '^not running'; check 'status: not running again' $? "$out"

echo '== serve in the foreground ends on the stop signal'
"$exe" serve > "$work/serve.out" 2>&1 &
spid=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do running && break; sleep 0.5; done
running; check 'serve runs' $? "$(cat "$work/serve.out")"
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
    # launchd gives the agent this user's real home folder, so the command that
    # stops it looks for its lock there too.
    mac() { HOME="$real_home" XDG_CACHE_HOME= "$@"; }
    domain="gui/$(id -u)"
    launchctl bootstrap "$domain" "$plist" > "$work/launchctl.out" 2>&1; bcode=$?
    if [ "$bcode" != 0 ]; then
      domain="user/$(id -u)"
      launchctl bootstrap "$domain" "$plist" >> "$work/launchctl.out" 2>&1; bcode=$?
    fi
    check "launchd takes the file ($domain)" "$bcode" "$(cat "$work/launchctl.out")"
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do mac running && break; sleep 0.5; done
    mac running; check 'the agent comes up under launchd' $? "$(launchctl print "$domain/app.askorus.agent" 2>&1 | head -n 30)"
    out="$(mac "$exe" shutdown 2>&1)"; has "$out" 'the agent stopped'; check 'shutdown stops the agent launchd started' $? "$out"
    sleep 1
    ! mac running; check 'launchd does not start it again' $?
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
      for _ in 1 2 3 4 5 6 7 8 9 10; do sys running && break; sleep 0.5; done
      sys running; check 'the agent comes up under systemd' $? "$(sys systemctl --user status askorus-agent.service 2>&1 | head -n 15)"
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

# An update as the phone's button makes it, of an agent that the system's
# service manager started (#71, R-175 to R-178).
update_under_service() {
  echo "== an update from the phone, of an agent that $1 started (#71)"
  local up="$work/up" home="$work/uphome" v code out
  uexe="$home/.local/bin/askorus-agent"
  files="$ASKORUS_AGENT_DIR/test-update"
  mkdir -p "$up" "$home/.local/bin"
  printf '{"Replace":{"%s":"%s"}}\n' "$root/agent/cmd/askorus-agent/zz_update_seams_test_build.go" "$root/scripts/testdata/update-seams.go.txt" > "$up/overlay.json"
  code=0
  for v in 9.0.1 9.0.2 9.0.3; do
    (cd "$root/agent" && CGO_ENABLED=0 HOME="$real_home" XDG_CACHE_HOME= XDG_CONFIG_HOME= go build -trimpath -overlay "$up/overlay.json" -tags ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail -ldflags "-X main.version=$v" -o "$up/$v" ./cmd/askorus-agent) || code=1
  done
  check 'the test releases build' "$code"
  [ "$code" = 0 ] || return
  cp "$up/9.0.1" "$uexe"
  out="$(service_on 2>&1)"; check "$1 takes the test agent" $? "$out"

  echo '   -- it updates'
  service_start
  svc_waits_running; check 'the agent runs' $?
  press 9.0.2
  [ "$(update_state)" = done ]; check 'the update ends as done' $? "$(update_detail)"
  svc running; check 'the new agent runs' $? "$(update_detail)"
  [ "$(svc "$uexe" version)" = 'askorus-agent 9.0.2' ]; check 'it is the new version' $?
  stays_stopped

  echo '   -- the old one does not stop: it is ended after a minute'
  cp "$up/9.0.1" "$up/old" && mv "$up/old" "$uexe"
  service_start
  svc_waits_running; check 'the agent runs' $?
  mkdir -p "$files"
  : > "$files/hang"
  press 9.0.2
  [ "$(update_state)" = done ]; check 'the update ends as done' $? "$(update_detail)"
  svc running; check 'the new agent runs' $? "$(update_detail)"
  rm -f "$files/hang"
  stays_stopped

  echo '   -- the new one dies just after it came up: back to the old one'
  cp "$up/9.0.2" "$up/old" && mv "$up/old" "$uexe"
  service_start
  svc_waits_running; check 'the agent runs' $?
  mkdir -p "$files"
  : > "$files/crash-9.0.3"
  press 9.0.3
  [ "$(update_state)" = rolled-back ]; check 'the update is rolled back' $? "$(update_detail)"
  svc_waits_running; check 'the old agent runs again' $? "$(update_detail)"
  [ "$(svc "$uexe" version)" = 'askorus-agent 9.0.2' ]; check 'it is the old version' $?
  rm -f "$files/crash-9.0.3"
  stays_stopped

  if [ "$1" = systemd ]; then
    echo '   -- the service next to an agent that start ran (R-176)'
    svc "$uexe" start > /dev/null 2>&1
    service_start
    sleep 7
    [ "$(svc systemctl --user show askorus-agent.service -p NRestarts)" = NRestarts=0 ]; check 'systemd does not start it again and again' $?
    stays_stopped
  fi
  service_off
}

update_state() { sed -n 's/.*"state": *"\([^"]*\)".*/\1/p' "$ASKORUS_AGENT_DIR/update-progress.json" 2>/dev/null; }
update_detail() { cat "$ASKORUS_AGENT_DIR/update-progress.json" 2>/dev/null; tail -n 5 "$ASKORUS_AGENT_DIR/logs/update.out" 2>/dev/null; }
# press <version>: the phone's button, and the wait for the update's end.
press() {
  "$work/up/$1" test-release "$files" "$1" "$work/up/$1"
  rm -f "$ASKORUS_AGENT_DIR/update.json" "$ASKORUS_AGENT_DIR/update-progress.json"
  printf '%s' "$1" > "$files/press"
  for _ in $(seq 1 160); do
    case "$(update_state)" in done | rolled-back | failed | not-taken) break ;; esac
    sleep 1
  done
}
svc_waits_running() {
  for _ in $(seq 1 30); do
    svc running && [ -s "$ASKORUS_AGENT_DIR/agent.pid" ] && return 0
    sleep 0.5
  done
  return 1
}
stays_stopped() {
  local out
  out="$(svc "$uexe" shutdown 2>&1)"; has "$out" 'the agent stopped'; check 'shutdown stops it' $? "$out"
  sleep 7 # the unit's RestartSec is 5
  ! svc running; check 'and nothing starts it again' $?
}

if [ "$os" = darwin ] && [ "${ASKORUS_TEST_LAUNCHD:-}" = 1 ]; then
  svc() { mac "$@"; }
  service_on() {
    "$uexe" autostart on || return 1
    # bootout of the test above may still be under way
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      launchctl bootstrap "$domain" "$plist" && return 0
      sleep 1
    done
    return 1
  }
  service_start() { launchctl kickstart "$domain/app.askorus.agent" > /dev/null 2>&1; }
  service_off() {
    launchctl bootout "$domain/app.askorus.agent" > /dev/null 2>&1
    "$uexe" autostart off > /dev/null 2>&1
  }
  if launchctl print "$domain/app.askorus.agent" > /dev/null 2>&1; then
    note 'launchd still has an agent of that name: left alone, the update part is skipped'
  else
    update_under_service launchd
  fi
elif [ "$os" = linux ] && [ "${ASKORUS_TEST_SYSTEMD:-}" = 1 ] && HOME="$real_home" XDG_CONFIG_HOME= systemctl --user show-environment > /dev/null 2>&1; then
  svc() { HOME="$real_home" XDG_CONFIG_HOME= XDG_CACHE_HOME= "$@"; }
  service_on() { svc "$uexe" autostart on; }
  service_start() { svc systemctl --user start askorus-agent.service > /dev/null 2>&1; }
  service_off() {
    svc "$uexe" autostart off > /dev/null 2>&1
    svc systemctl --user stop askorus-agent.service > /dev/null 2>&1
    svc systemctl --user reset-failed askorus-agent.service > /dev/null 2>&1
  }
  if [ -e "$real_home/.config/systemd/user/askorus-agent.service" ]; then
    note 'this user already has the unit: left alone, the update part is skipped'
  else
    update_under_service systemd
  fi
fi

echo '== check runs and names this system'"'"'s way to start'
out="$("$exe" check 2>&1)"
has "$out" 'normal user rights' && has "$out" 'askorus-agent start'; check 'check lists what is missing, with start' $? "$(printf '%s' "$out" | head -n 12)"
! has "$out" 'only supported on Windows'; check 'nothing says Windows only' $? "$out"

uexe="$exe"
"$exe" shutdown > /dev/null 2>&1
if [ "$keep" = 1 ]; then echo "kept: $work"; else rm -rf "$work"; fi
if [ "$fails" = 0 ]; then echo 'ALL PASSED'; else echo "FAILED: $fails"; exit 1; fi
