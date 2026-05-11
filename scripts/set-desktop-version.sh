#!/bin/sh
set -eu

version="${1:?usage: sh scripts/set-desktop-version.sh <version> [desktop-dir]}"
desktop_dir="${2:-picoaide-desktop}"

case "$version" in
  v*) version="${version#v}" ;;
esac

case "$version" in
  ""|*[!A-Za-z0-9._+-]*)
    echo "invalid desktop version: $version" >&2
    exit 1
    ;;
esac

{
  printf '%s\n' '"""PicoAide Desktop build metadata."""'
  printf '\n'
  printf 'VERSION = "%s"\n' "$version"
} > "$desktop_dir/version.py"
