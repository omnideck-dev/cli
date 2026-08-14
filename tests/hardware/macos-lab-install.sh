#!/usr/bin/env bash

set -Eeuo pipefail

export LANG=C
export LC_ALL=C

source_binary="${1:?Staged CLI path is required}"
evidence_file="${2:-}"
[[ "$(uname -s)/$(uname -m)" == Darwin/arm64 ]] || {
  printf 'The macOS lab installer requires native Apple Silicon.\n' >&2
  exit 2
}
[[ -x "$source_binary" ]] || { printf 'CLI candidate is not executable: %s\n' "$source_binary" >&2; exit 1; }
file "$source_binary" | grep -Eq 'Mach-O 64-bit.*arm64'

managed_root="$HOME/.omnideck-lab"
destination="$managed_root/bin/omnideck"
marker="$managed_root/managed-cli.sha256"
[[ ! -e "$destination" && ! -L "$destination" ]] || {
  printf 'The disposable baseline was not clean; CLI already exists: %s\n' "$destination" >&2
  exit 1
}
mkdir -p "$(dirname "$destination")" "$managed_root"
install -m 0755 "$source_binary" "$destination"
shasum -a 256 "$destination" > "$marker"
shasum -a 256 --check "$marker" >/dev/null
if [[ -n "$evidence_file" ]]; then
  mkdir -p "$(dirname "$evidence_file")"
  digest="$(shasum -a 256 "$destination" | awk '{print $1}')"
  version="$($destination --version)"
  node - "$evidence_file" "$destination" "$digest" "$version" <<'NODE'
const fs = require('node:fs');
const [path, destination, sha256, version] = process.argv.slice(2);
fs.writeFileSync(path, `${JSON.stringify({
  schemaVersion: 1,
  kind: 'cli',
  destination,
  sha256,
  version,
  architecture: 'arm64',
}, null, 2)}\n`);
NODE
fi
printf 'Installed CLI candidate: %s\n' "$destination"
