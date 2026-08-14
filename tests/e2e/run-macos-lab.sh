#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
lab_dir="${OMNIDECK_VM_LAB_DIR:-}"
target=macos-arm64
profile="${OMNIDECK_VM_LAB_PROFILE:-release-clean}"
builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:local}"

[[ -n "$lab_dir" && -x "$lab_dir/lab.sh" ]] || {
  printf 'Set OMNIDECK_VM_LAB_DIR to the deployed OmniDeck VM lab.\n' >&2
  exit 2
}

if [[ "${OMNIDECK_VM_LAB_LEASED:-}" != 1 ]]; then
  "$lab_dir/lab.sh" preflight cli "$profile" --lanes "$target" >/dev/null
  command -v docker >/dev/null 2>&1 || { printf 'Docker is required to build macOS ARM64 artifacts off-host.\n' >&2; exit 2; }
  docker image inspect "$builder_image" >/dev/null 2>&1 || {
    printf 'Builder image %q is unavailable. Build .devcontainer/Dockerfile first.\n' "$builder_image" >&2
    exit 2
  }

  fixture_key="$(shasum -a 256 "$repo_root/tests/hardware/fixture/Containerfile" "$repo_root/tests/hardware/fixture/index.html" | shasum -a 256 | awk '{print substr($1,1,20)}')"
  fixture_cache="$($lab_dir/lab.sh cache-path cli "macos-arm64-fixture-${fixture_key}")"
  fixture_archive="$fixture_cache/fixture.tar"
  fixture_image="localhost/omnideck-hardware-fixture:macos-arm64-${fixture_key}"
  if [[ ! -f "$fixture_archive" ]]; then
    mkdir -p "$fixture_cache"
    temporary_archive="$fixture_cache/.fixture.tar.$$"
    docker buildx build --platform linux/arm64 \
      --file "$repo_root/tests/hardware/fixture/Containerfile" --tag "$fixture_image" \
      --output "type=docker,dest=${temporary_archive}" "$repo_root/tests/hardware/fixture"
    mv -- "$temporary_archive" "$fixture_archive"
    shasum -a 256 "$fixture_archive" > "$fixture_cache/SHA256SUMS"
  fi

  run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  source_commit="$(git -C "$repo_root" rev-parse HEAD)"
  source_dirty=false
  [[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=normal)" ]] || source_dirty=true
  output_dir="$($lab_dir/lab.sh artifact-path cli macos-e2e "$run_id")"
  build_dir="$output_dir/build"
  builder_cache="$($lab_dir/lab.sh cache-path cli macos-go-builder-v1)"
  mkdir -p "$build_dir/archive"
  mkdir -p "$builder_cache/gocache" "$builder_cache/gopath"
  short_commit="${source_commit:0:12}"
  expected_version="vm-e2e-${short_commit}"
  ldflags="-X main.version=${expected_version} -X main.commit=${short_commit} -X main.date=vm-e2e"

  docker run --rm --entrypoint /bin/zsh \
    --user "$(id -u):$(id -g)" \
    --env GOOS=darwin --env GOARCH=arm64 --env CGO_ENABLED=0 \
    --env GOCACHE=/tmp/omnideck-go-build --env GOPATH=/tmp/omnideck-go \
    --volume "$repo_root:/workspace:ro" --volume "$build_dir:/out" \
    --volume "$builder_cache/gocache:/tmp/omnideck-go-build" \
    --volume "$builder_cache/gopath:/tmp/omnideck-go" \
    --workdir /workspace "$builder_image" \
    -c "go build -trimpath -buildvcs=false -ldflags \"${ldflags}\" -o /out/archive/omnideck . && go build -trimpath -buildvcs=false -o /out/releasecontract ./tests/releasecontract"
  chmod +x "$build_dir/archive/omnideck" "$build_dir/releasecontract"
  file "$build_dir/archive/omnideck" "$build_dir/releasecontract" | grep -Eq 'Mach-O 64-bit.*arm64'
  tar -czf "$build_dir/omnideck-darwin-arm64.tar.gz" -C "$build_dir/archive" omnideck
  (cd "$build_dir" && shasum -a 256 omnideck-darwin-arm64.tar.gz > SHA256SUMS)
  tar --sort=name --mtime="@$(git -C "$repo_root" show -s --format=%ct HEAD)" \
    --owner=0 --group=0 --numeric-owner -cf - -C "$repo_root" contracts | gzip -n > "$build_dir/contracts.tar.gz"

  "$lab_dir/lab.sh" evidence-init "$output_dir" cli macos-e2e "$run_id" \
    "$source_commit" "$target" runtime-ready "sourceDirty=$source_dirty" \
    "architecture=arm64" "podmanSetup=excluded-ready-runtime" "driver=pty"
  trap '"$lab_dir/lab.sh" evidence-finish "$output_dir" failed >/dev/null 2>&1 || true' EXIT
  status=0
  "$lab_dir/lab.sh" lease "$target" cli-e2e "$run_id" --cleanup-baseline runtime-ready -- env \
    OMNIDECK_MACOS_CLI_BUILD_DIR="$build_dir" \
    OMNIDECK_MACOS_FIXTURE_ARCHIVE="$fixture_archive" \
    OMNIDECK_MACOS_FIXTURE_IMAGE="$fixture_image" \
    OMNIDECK_MACOS_EXPECTED_VERSION="$expected_version" \
    OMNIDECK_VM_LAB_OUTPUT_DIR="$output_dir" "$0" || status=$?
  trap - EXIT
  [[ "$status" == 0 ]] && evidence_status=passed || evidence_status=failed
  "$lab_dir/lab.sh" evidence-finish "$output_dir" "$evidence_status"
  printf 'Evidence: %s\n' "$output_dir"
  exit "$status"
fi

[[ "${OMNIDECK_VM_LAB_VM:-}" == "$target" ]] || { printf 'The active lease does not own %s.\n' "$target" >&2; exit 2; }
build_dir="${OMNIDECK_MACOS_CLI_BUILD_DIR:?Prepared macOS CLI build is required}"
fixture_archive="${OMNIDECK_MACOS_FIXTURE_ARCHIVE:?Prepared ARM64 fixture is required}"
fixture_image="${OMNIDECK_MACOS_FIXTURE_IMAGE:?Prepared fixture image name is required}"
expected_version="${OMNIDECK_MACOS_EXPECTED_VERSION:?Expected CLI version is required}"
output_dir="${OMNIDECK_VM_LAB_OUTPUT_DIR:?Lab evidence directory is required}"
safe_run_id="$(printf '%s' "$OMNIDECK_VM_LAB_RUN_ID" | tr -cd '[:alnum:]_.-')"
remote_root="/private/tmp/omnideck-cli-macos-e2e-${safe_run_id}"
remote_staged=0

cleanup_remote() {
  local status=$?
  set +e
  if [[ "$remote_staged" == 1 ]]; then
    case "$remote_root" in
      /private/tmp/omnideck-cli-macos-e2e-[[:alnum:]_.-]*)
        "$lab_dir/lab.sh" run "$target" rm -rf -- "$remote_root" >/dev/null 2>&1 || true ;;
      *) printf 'Refusing to remove unexpected remote path: %s\n' "$remote_root" >&2; status=1 ;;
    esac
  fi
  exit "$status"
}
trap cleanup_remote EXIT

"$lab_dir/lab.sh" reset "$target" runtime-ready
"$lab_dir/lab.sh" verify "$target"
"$lab_dir/lab.sh" run "$target" mkdir -p "$remote_root"
remote_staged=1
for source in omnideck-darwin-arm64.tar.gz SHA256SUMS releasecontract contracts.tar.gz; do
  "$lab_dir/lab.sh" copy-to "$target" "$build_dir/$source" "$remote_root/$source"
done
"$lab_dir/lab.sh" copy-to "$target" "$fixture_archive" "$remote_root/fixture.tar"
"$lab_dir/lab.sh" copy-to "$target" "$script_dir/macos_guest.sh" "$remote_root/macos_guest.sh"
"$lab_dir/lab.sh" copy-to "$target" "$script_dir/terminal_driver.py" "$remote_root/terminal_driver.py"
"$lab_dir/lab.sh" copy-to "$target" "$repo_root/tests/hardware/run.sh" "$remote_root/hardware-run.sh"
"$lab_dir/lab.sh" copy-to "$target" "$repo_root/tests/hardware/fixture" "$remote_root/hardware-fixture"
"$lab_dir/lab.sh" run "$target" chmod +x "$remote_root/macos_guest.sh" \
  "$remote_root/terminal_driver.py" "$remote_root/releasecontract" "$remote_root/hardware-run.sh"

test_status=0
"$lab_dir/lab.sh" run "$target" "$remote_root/macos_guest.sh" \
  "$remote_root" "$expected_version" "$fixture_image" || test_status=$?
mkdir -p "$output_dir/evidence"
"$lab_dir/lab.sh" copy-from "$target" "$remote_root/results/." "$output_dir/evidence/" || test_status=1

if [[ -f "$output_dir/evidence/summary.json" ]]; then
  python3 - "$output_dir/evidence/summary.json" <<'PY' || test_status=1
import json, sys
summary=json.load(open(sys.argv[1], encoding='utf-8'))
assert summary['status'] == 'passed', summary
assert summary['platform'] == 'darwin' and summary['architecture'] == 'arm64', summary
assert summary['podmanSetup'] == 'excluded-ready-runtime', summary
PY
fi
exit "$test_status"
