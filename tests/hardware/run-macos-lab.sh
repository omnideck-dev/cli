#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
lab_dir="${OMNIDECK_VM_LAB_DIR:-}"
target=macos-arm64
profile="${OMNIDECK_VM_LAB_PROFILE:-release-clean}"
builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:local}"

[[ -n "$lab_dir" && -x "$lab_dir/lab.sh" ]] || {
  printf 'Set OMNIDECK_VM_LAB_DIR to the deployed OmniDeck VM lab.\n' >&2
  exit 2
}

python3 -c 'import json,subprocess,sys; data=json.loads(subprocess.check_output([sys.argv[1], "capabilities", "--json"])); assert "remote-hosts" in data["features"]' "$lab_dir/lab.sh" || {
  printf 'The deployed lab controller does not support physical hosts.\n' >&2
  exit 2
}

if [[ "${OMNIDECK_VM_LAB_LEASED:-}" != 1 ]]; then
  "$lab_dir/lab.sh" preflight cli "$profile" --lanes "$target" >/dev/null
  command -v docker >/dev/null 2>&1 || { printf 'Docker is required to run the pinned Go builder.\n' >&2; exit 2; }
  docker image inspect "$builder_image" >/dev/null 2>&1 || {
    printf 'Builder image %q is unavailable. Build .devcontainer/Dockerfile first.\n' "$builder_image" >&2
    exit 2
  }

  fixture_key="$(shasum -a 256 "$script_dir/fixture/Containerfile" "$script_dir/fixture/index.html" | shasum -a 256 | awk '{print substr($1,1,20)}')"
  fixture_cache="$($lab_dir/lab.sh cache-path cli "macos-arm64-fixture-${fixture_key}")"
  fixture_archive="$fixture_cache/fixture.tar"
  fixture_image="localhost/omnideck-hardware-fixture:macos-arm64-${fixture_key}"
  if [[ ! -f "$fixture_archive" ]]; then
    mkdir -p "$fixture_cache"
    temporary_archive="$fixture_cache/.fixture.tar.$$"
    docker buildx build --platform linux/arm64 \
      --file "$script_dir/fixture/Containerfile" --tag "$fixture_image" \
      --output "type=docker,dest=${temporary_archive}" "$script_dir/fixture"
    mv -- "$temporary_archive" "$fixture_archive"
    shasum -a 256 "$fixture_archive" > "$fixture_cache/SHA256SUMS"
  fi

  run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  source_commit="$(git -C "$repo_root" rev-parse HEAD)"
  source_dirty=false
  [[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=normal)" ]] || source_dirty=true
  output_dir="$($lab_dir/lab.sh artifact-path cli macos-hardware "$run_id")"
  build_dir="$output_dir/build"
  mkdir -p "$build_dir"
  binary="$build_dir/omnideck"
  short_commit="${source_commit:0:12}"
  ldflags="-X main.version=local-macos-arm64 -X main.commit=${short_commit}"

  docker run --rm --entrypoint /bin/zsh \
    --user "$(id -u):$(id -g)" \
    --env GOOS=darwin --env GOARCH=arm64 --env CGO_ENABLED=0 \
    --env GOCACHE=/tmp/omnideck-go-build --env GOPATH=/tmp/omnideck-go \
    --volume "$repo_root:/workspace:ro" --volume "$build_dir:/out" \
    --workdir /workspace "$builder_image" \
    -c "go build -trimpath -buildvcs=false -ldflags \"${ldflags}\" -o /out/omnideck ."
  chmod +x "$binary"
  file "$binary" | grep -Eq 'Mach-O 64-bit.*arm64'
  shasum -a 256 "$binary" > "$output_dir/SHA256SUMS"

  "$lab_dir/lab.sh" evidence-init "$output_dir" cli macos-hardware "$run_id" \
    "$source_commit" "$target" runtime-ready "sourceDirty=$source_dirty" "architecture=arm64" "artifactKind=source-build"
  trap '"$lab_dir/lab.sh" evidence-finish "$output_dir" failed >/dev/null 2>&1 || true' EXIT
  status=0
  "$lab_dir/lab.sh" lease "$target" cli "$run_id" --cleanup-baseline runtime-ready -- env \
    OMNIDECK_MACOS_CLI_BINARY="$binary" \
    OMNIDECK_MACOS_FIXTURE_ARCHIVE="$fixture_archive" \
    OMNIDECK_MACOS_FIXTURE_IMAGE="$fixture_image" \
    OMNIDECK_VM_LAB_OUTPUT_DIR="$output_dir" \
    "$0" || status=$?
  trap - EXIT
  if [[ "$status" == 0 ]]; then
    "$lab_dir/lab.sh" evidence-finish "$output_dir" passed
  else
    "$lab_dir/lab.sh" evidence-finish "$output_dir" failed
  fi
  printf 'Evidence: %s\n' "$output_dir"
  exit "$status"
fi

[[ "${OMNIDECK_VM_LAB_VM:-}" == "$target" ]] || {
  printf 'The active lab lease does not own %s.\n' "$target" >&2
  exit 2
}
binary="${OMNIDECK_MACOS_CLI_BINARY:?Prepared macOS CLI binary is required}"
output_dir="${OMNIDECK_VM_LAB_OUTPUT_DIR:?Lab evidence directory is required}"
fixture_archive="${OMNIDECK_MACOS_FIXTURE_ARCHIVE:?Prepared ARM64 fixture archive is required}"
fixture_image="${OMNIDECK_MACOS_FIXTURE_IMAGE:?Prepared ARM64 fixture image name is required}"
safe_run_id="$(printf '%s' "${OMNIDECK_VM_LAB_RUN_ID}" | tr -cd '[:alnum:]_.-')"
remote_root="/tmp/omnideck-cli-macos-${safe_run_id}"
remote_staged=0

cleanup_remote() {
  local status=$?
  if [[ "$remote_staged" == 1 ]]; then
    case "$remote_root" in
      /tmp/omnideck-cli-macos-[[:alnum:]_.-]*) "$lab_dir/lab.sh" run "$target" rm -rf -- "$remote_root" >/dev/null 2>&1 || true ;;
      *) printf 'Refusing to remove unexpected remote path: %s\n' "$remote_root" >&2; status=1 ;;
    esac
  fi
  exit "$status"
}
trap cleanup_remote EXIT

"$lab_dir/lab.sh" reset "$target" runtime-ready
"$lab_dir/lab.sh" verify "$target"
remote_home="$("$lab_dir/lab.sh" run "$target" /bin/zsh -c 'printf %s "$HOME"')"
case "$remote_home" in /Users/*) ;; *) printf 'Unexpected remote home: %s\n' "$remote_home" >&2; exit 1 ;; esac
"$lab_dir/lab.sh" run "$target" mkdir -p "$remote_root"
remote_staged=1
"$lab_dir/lab.sh" copy-to "$target" "$binary" "$remote_root/omnideck"
"$lab_dir/lab.sh" copy-to "$target" "$fixture_archive" "$remote_root/fixture.tar"
"$lab_dir/lab.sh" copy-to "$target" "$script_dir" "$remote_root/hardware"
"$lab_dir/lab.sh" run "$target" chmod +x "$remote_root/omnideck" \
  "$remote_root/hardware/run.sh" "$remote_root/hardware/macos-lab-install.sh"
"$lab_dir/lab.sh" run "$target" "$remote_root/hardware/macos-lab-install.sh" \
  "$remote_root/omnideck" "$remote_root/artifacts/install.json"

test_status=0
"$lab_dir/lab.sh" run "$target" env \
  OMNIDECK_HARDWARE_CLI="$remote_home/.omnideck-lab/bin/omnideck" \
  OMNIDECK_HARDWARE_ENGINE=podman \
  OMNIDECK_HARDWARE_REGISTRY_PORT=46864 \
  OMNIDECK_HARDWARE_TEST_IMAGE_ARCHIVE="$remote_root/fixture.tar" \
  OMNIDECK_HARDWARE_ARCHIVE_IMAGE="$fixture_image" \
  OMNIDECK_HARDWARE_OUTPUT_DIR="$remote_root/artifacts" \
  "$remote_root/hardware/run.sh" || test_status=$?

mkdir -p "$output_dir/hardware"
if ! "$lab_dir/lab.sh" copy-from "$target" "$remote_root/artifacts/." "$output_dir/hardware/"; then
  [[ "$test_status" != 0 ]] || test_status=1
fi
if ! python3 - "$output_dir/hardware/install.json" "$binary" <<'PY'
import hashlib, json, sys
with open(sys.argv[1]) as handle:
    installation = json.load(handle)
with open(sys.argv[2], "rb") as handle:
    expected = hashlib.sha256(handle.read()).hexdigest()
assert installation["kind"] == "cli"
assert installation["destination"].endswith("/.omnideck-lab/bin/omnideck")
assert installation["architecture"] == "arm64"
assert installation["sha256"] == expected
PY
then
  [[ "$test_status" != 0 ]] || test_status=1
fi
exit "$test_status"
