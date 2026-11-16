#!/usr/bin/env bash
# Builds the Tailscale bridge for the Android app (#57) and puts it where
# the app's native module expects it:
#   app/modules/tailnet/android/libs/tsbridge.jar            (the Java side)
#   app/modules/tailnet/android/src/main/jniLibs/<abi>/libgojni.so
# Neither is kept in git: run this before building the app, and again when
# tsbridge/ or its Tailscale version changes.
#
#   build-android.sh [abis]     abis: arm64 (default), or a list such as
#                               arm64,arm,amd64,386 (all four for a bundle)
#
# Needs Go, gomobile and gobind (go install golang.org/x/mobile/cmd/gomobile@latest
# golang.org/x/mobile/cmd/gobind@latest), the Android SDK with an NDK, and a
# JDK: ANDROID_HOME, ANDROID_NDK_HOME and JAVA_HOME must be set.
set -euo pipefail
abis="${1:-arm64}"
here="$(cd "$(dirname "$0")" && pwd)"
module="$here/../app/modules/tailnet/android"
: "${ANDROID_HOME:?set ANDROID_HOME to the Android SDK}"
: "${ANDROID_NDK_HOME:?set ANDROID_NDK_HOME to an NDK inside it}"

targets=""
for a in ${abis//,/ }; do
  targets="${targets:+$targets,}android/$a"
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$here"
# -s -w: no debug tables (the library is the largest part of the app as it is).
# max-page-size: Android 15 devices with 16 KB memory pages load only
# libraries laid out for them, and Google Play asks for it.
# -tags: the Tailscale library without the parts the app must not carry
# (buildTags in tsbridge.go: its service for other devices, diagnostics,
# the log uploader).
# -trimpath: module paths instead of this PC's file paths (#61). Without it
# Go writes where each source file was, and the toolchain and modules sit
# in the Go module cache under the user's folder.
# -X runtime.modinfo=: no module list in the library (#61). gomobile builds
# from a module of its own that points at this folder by its full path
# ("=> <this checkout>/tsbridge"), and Go writes that line into the module
# list. Nothing reads the list: Tailscale reads only its git fields, which a
# gomobile build does not have, and the app's license list comes from
# go list (app/scripts/go-libs.mjs).
gomobile bind -target="$targets" -androidapi 24 -javapkg app.askorus.tailnet \
  -trimpath \
  -tags=ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail \
  -ldflags="-s -w -X=runtime.modinfo= -extldflags=-Wl,-z,max-page-size=16384" \
  -o "$work/tsbridge.aar" .

mkdir -p "$work/x" "$work/j" "$module/libs" "$module/src/main/jniLibs"
(cd "$work/x" && unzip -q ../tsbridge.aar)
(cd "$work/j" && unzip -q ../x/classes.jar)

# Nothing of this PC's folders may be in what was built (#61): the Go module
# cache, the Go toolchain, the user's folder and this checkout, written as
# Go writes paths (forward slashes; on Windows with the drive). A build that
# still has one is not put into the app. The message names which folder,
# not the path.
native() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s\n' "$1"; fi; }
leaks=0
check_path() {
  local what="$1" p
  p="$(native "$2")"
  [ "${#p}" -ge 4 ] || return 0
  if grep -a -r -F -i -q -e "$p" -e "${p//\//\\}" "$work/x/jni" "$work/j"; then
    echo "build-android.sh: the built bridge holds the path of $what (#61)" >&2
    leaks=1
  fi
}
check_path "the Go module cache" "$(go env GOMODCACHE)"
check_path "the Go toolchain" "$(go env GOROOT)"
check_path "the user's folder" "$HOME"
check_path "this checkout" "$(cd "$here/.." && pwd)"
if [ "$leaks" -ne 0 ]; then
  echo "build-android.sh: not using this build" >&2
  exit 1
fi

cp "$work/x/classes.jar" "$module/libs/tsbridge.jar"
rm -rf "$module/src/main/jniLibs"/*
cp -r "$work/x/jni/"* "$module/src/main/jniLibs/"
# What was built, so a later build can tell a stale library from a fresh one.
{
  echo "abis=$abis"
  echo "tailscale=$(go list -m -f '{{.Version}}' tailscale.com)"
  echo "go=$(go env GOVERSION)"
} > "$module/libs/tsbridge.txt"
ls -l "$module/libs" "$module/src/main/jniLibs"/*
