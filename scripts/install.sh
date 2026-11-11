#!/bin/sh
# Installs or updates the Askorus PC agent on macOS and Linux (#52), the
# counterpart of install.ps1:
#
#   curl -fsSL https://github.com/heeo-dev/askorus/releases/latest/download/install.sh | sh
#
# It downloads the agent for this system from the GitHub release, checks it
# against the release's SHA256SUMS.txt, puts it in ~/.local/bin and runs
# `askorus-agent setup`, which walks through the rest. Run again later, it
# updates: it stops a running agent, keeps the old program as
# askorus-agent.prev and starts the new one.
#
# It needs no administrator rights and must not be run with sudo: the agent
# runs as you, with your AI sign-ins. It changes nothing outside the install
# folder; what setup changes, setup says first.
#
#   ASKORUS_VERSION=0.2.0     a release other than the latest
#   ASKORUS_INSTALL_DIR=...   another folder than ~/.local/bin
#   ASKORUS_NO_SETUP=1        install only; run `askorus-agent setup` yourself
#   ASKORUS_DOWNLOAD_BASE=... an https address or a folder that holds the
#                             release files (for testing)
#
# Plain sh, and everything inside main, called on the last line: a download
# that breaks off in the middle runs nothing.

set -eu

say() { printf 'askorus: %s\n' "$*"; }
die() { printf 'askorus: %s\n' "$*" >&2; exit 1; }

main() {
  tmp=''
  new=''
  trap 'cleanup' EXIT
  trap 'exit 1' INT TERM HUP

  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "This installer is for macOS and Linux (this is $(uname -s)). On Windows use install.ps1." ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "Askorus needs a 64-bit Intel/AMD or ARM computer (this one is $(uname -m))." ;;
  esac
  if [ "$(id -u)" = 0 ]; then
    die 'Run this as yourself, without sudo: the agent runs with normal rights and your AI sign-ins.'
  fi
  asset="askorus-agent-$os-$arch"

  base="${ASKORUS_DOWNLOAD_BASE:-}"
  from="$base (ASKORUS_DOWNLOAD_BASE)"
  if [ -z "$base" ]; then
    base='https://github.com/heeo-dev/askorus/releases/latest/download'
    if [ -n "${ASKORUS_VERSION:-}" ]; then
      base="https://github.com/heeo-dev/askorus/releases/download/v${ASKORUS_VERSION#v}"
    fi
    from="$base"
  fi
  case "$base" in
    https://*) ;;
    *://*) die "ASKORUS_DOWNLOAD_BASE must be an https address or a folder, not $base" ;;
  esac

  dir="${ASKORUS_INSTALL_DIR:-$HOME/.local/bin}"
  mkdir -p "$dir"
  dir="$(cd "$dir" && pwd -P)"
  exe="$dir/askorus-agent"

  tmp="$(mktemp -d "${TMPDIR:-/tmp}/askorus-install-XXXXXXXX")"
  say "downloading $asset from $from"
  fetch "$base" "$asset" "$tmp/$asset"
  fetch "$base" SHA256SUMS.txt "$tmp/SHA256SUMS.txt"

  # "<sha-256>  <file name>" (a * before the name is allowed)
  want="$(awk -v f="$asset" 'NF == 2 { n = $2; sub(/^\*/, "", n); if (n == f) print $1 }' "$tmp/SHA256SUMS.txt" | head -n 1)"
  [ -n "$want" ] || die "SHA256SUMS.txt has no line for $asset; nothing was installed."
  got="$(sha256 "$tmp/$asset")"
  if [ "$got" != "$want" ]; then
    die "The download is damaged or is not the released file (SHA-256 $got, the release says $want); nothing was installed."
  fi
  say 'the SHA-256 matches the release'

  new="$dir/askorus-agent.new"
  mv -f "$tmp/$asset" "$new"
  chmod 755 "$new"
  said="$("$new" version 2>/dev/null || true)"
  case "$said" in
    'askorus-agent '*' '*) die "The downloaded agent does not run on this computer ($said); nothing was changed." ;;
    'askorus-agent '?*) ;;
    *) die "The downloaded agent does not run on this computer ($said); nothing was changed." ;;
  esac

  if [ ! -e "$exe" ]; then
    mv "$new" "$exe"
    new=''
    say "installed $said in $dir"
    if [ "${ASKORUS_NO_SETUP:-}" = 1 ]; then
      say "next, run: \"$exe\" setup"
      return 0
    fi
    run_setup "$exe"
    return 0
  fi

  old="$("$exe" version 2>/dev/null || echo 'an agent that does not run')"
  running=0
  case "$("$exe" status 2>/dev/null | head -n 1)" in
    running*) running=1 ;;
  esac
  if [ "$running" = 1 ]; then
    say 'stopping the agent to update it'
    "$exe" shutdown >/dev/null || die 'The agent did not stop, so nothing was changed; run "askorus-agent shutdown" and then this again.'
  fi
  prev="$dir/askorus-agent.prev"
  if ! { rm -f "$prev" && mv "$exe" "$prev"; }; then
    back=''
    if [ "$running" = 1 ]; then
      if "$exe" start >/dev/null 2>&1; then back=' and runs again'; else back=', but did not start again: run "askorus-agent start"'; fi
    fi
    die "Could not put the new agent in place; the old one stays$back."
  fi
  if ! mv "$new" "$exe"; then
    mv "$prev" "$exe"
    back=''
    if [ "$running" = 1 ]; then
      if "$exe" start >/dev/null 2>&1; then back=' and runs again'; else back=', but did not start again: run "askorus-agent start"'; fi
    fi
    die "Could not put the new agent in place; the old one stays$back."
  fi
  new=''
  say "updated $old to $said in $dir (the old one is askorus-agent.prev)"
  if [ "$running" = 1 ]; then
    "$exe" start || die 'The new agent did not start (see above); the old one is askorus-agent.prev in the same folder.'
  fi
  if [ "${ASKORUS_NO_SETUP:-}" != 1 ] && [ "$("$exe" autostart status 2>/dev/null | head -n 1)" = off ]; then
    say 'setup did not finish before; running it again'
    run_setup "$exe"
  fi
}

# fetch <base> <name> <to>: from an https address or a folder.
fetch() {
  case "$1" in
    https://*)
      if command -v curl >/dev/null 2>&1; then
        curl -fsSL --proto '=https' --tlsv1.2 -o "$3" "$1/$2" || die "Could not download $2 from $1."
      elif command -v wget >/dev/null 2>&1; then
        wget -q --https-only -O "$3" "$1/$2" || die "Could not download $2 from $1."
      else
        die 'This needs curl or wget to download the agent.'
      fi
      ;;
    *) cp "$1/$2" "$3" 2>/dev/null || die "$1 has no file $2." ;;
  esac
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{ print $1 }'
  else
    die 'This needs sha256sum or shasum to check the download.'
  fi
}

# setup asks questions: when this script came through a pipe (curl | sh),
# it reads them from the terminal instead.
run_setup() {
  if [ -t 0 ]; then
    "$1" setup || say 'setup stopped before it finished: fix what it says, then run "askorus-agent setup" (or this installer) again'
  elif [ -r /dev/tty ] && [ -w /dev/tty ]; then
    "$1" setup </dev/tty || say 'setup stopped before it finished: fix what it says, then run "askorus-agent setup" (or this installer) again'
  else
    say "next, in a terminal run: \"$1\" setup"
  fi
}

cleanup() {
  if [ -n "$new" ] && [ -e "$new" ]; then rm -f "$new"; fi
  if [ -n "$tmp" ] && [ -d "$tmp" ]; then rm -rf "$tmp"; fi
}

main "$@"
