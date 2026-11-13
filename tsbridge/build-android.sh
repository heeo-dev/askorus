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
gomobile bind -target="$targets" -androidapi 24 -javapkg app.askorus.tailnet \
  -tags=ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail \
  -ldflags="-s -w -extldflags=-Wl,-z,max-page-size=16384" \
  -o "$work/tsbridge.aar" .

mkdir -p "$work/x" "$module/libs" "$module/src/main/jniLibs"
(cd "$work/x" && unzip -q ../tsbridge.aar)
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
