#!/usr/bin/env bash
set -euo pipefail

[[ "$(uname -s)" == Darwin ]] || { echo 'Signing requires macOS' >&2; exit 2; }
binary="${1:?Usage: sign-and-notarize.sh /path/to/omnideck}"
[[ -f "$binary" ]] || { echo 'CLI executable is missing' >&2; exit 2; }
for variable in APPLE_SIGNING_IDENTITY APPLE_TEAM_ID APPLE_API_KEY APPLE_API_ISSUER APPLE_API_KEY_PATH; do
  [[ -n "${!variable:-}" ]] || { echo "Required signing input $variable is not set" >&2; exit 1; }
done
binary="$(cd "$(dirname "$binary")" && pwd)/$(basename "$binary")"
workdir="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/omnideck-notary.XXXXXX")"
trap 'rm -rf -- "$workdir"' EXIT
keychain_args=()
if [[ -n "${OMNIDECK_SIGNING_KEYCHAIN:-}" ]]; then
  keychain_args=(--keychain "$OMNIDECK_SIGNING_KEYCHAIN")
fi
codesign "${keychain_args[@]}" --force --options runtime --timestamp --identifier dev.omnideck.cli \
  --sign "$APPLE_SIGNING_IDENTITY" "$binary"
codesign --verify --strict --verbose=4 "$binary"
details="$(codesign --display --verbose=4 "$binary" 2>&1)"
printf '%s\n' "$details"
grep -q '^Authority=Developer ID Application:' <<<"$details"
grep -q "^TeamIdentifier=${APPLE_TEAM_ID}$" <<<"$details"
grep -Eq '(^|[[:space:]])flags=.*\(runtime\)' <<<"$details"
grep -Eq '^Timestamp=.+$' <<<"$details"
! grep -q '^Timestamp=none$' <<<"$details"
# Apple accepts ZIP submissions; the published tar.gz contains these exact bytes.
# Standalone Mach-O executables and ZIPs cannot carry stapled tickets.
ditto -c -k "$binary" "$workdir/omnideck.zip"
notary_status=0
xcrun notarytool submit "$workdir/omnideck.zip" \
  --key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY" --issuer "$APPLE_API_ISSUER" \
  --wait --timeout 20m --output-format json > "$workdir/submission.json" || notary_status=$?
cat "$workdir/submission.json"
[[ "$notary_status" == 0 ]] || { echo "Notarization command failed or timed out; retain the submission ID above for follow-up" >&2; exit "$notary_status"; }
python3 - "$workdir/submission.json" <<'PY'
import json, sys
submission = json.load(open(sys.argv[1]))
if submission.get('status') != 'Accepted':
    raise SystemExit('Apple did not accept the signed CLI')
PY
codesign --verify --strict --check-notarization --verbose=4 "$binary"
