#!/bin/sh
set -eu

version="${1:?usage: sh scripts/package-extension.sh <version> <output-zip>}"
output="${2:?usage: sh scripts/package-extension.sh <version> <output-zip>}"

case "$version" in
  v*) version="${version#v}" ;;
esac

case "$version" in
  ""|*[!0-9.]*|.*|*.)
    echo "invalid extension version: $version" >&2
    exit 1
    ;;
esac

case "$version" in
  *..*)
    echo "invalid extension version: $version" >&2
    exit 1
    ;;
esac

parts=$(printf "%s" "$version" | awk -F. '{ print NF }')
if [ "$parts" -gt 4 ]; then
  echo "invalid extension version: $version" >&2
  exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

case "$output" in
  /*) output_abs="$output" ;;
  *) output_abs="$(pwd)/$output" ;;
esac

cp -R picoaide-extension/. "$tmpdir/"
mkdir -p "$(dirname "$output_abs")"
rm -f "$output_abs"

python3 - "$tmpdir" "$version" "$output_abs" <<'PY'
import json
import pathlib
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
output = pathlib.Path(sys.argv[3])

manifest_path = root / "manifest.json"
manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
manifest["version"] = version
manifest_path.write_text(
    json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
    encoding="utf-8",
)

with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(root.rglob("*")):
        if path.is_file():
            archive.write(path, path.relative_to(root).as_posix())
PY
