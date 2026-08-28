#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
source "${script_dir}/_common.sh"
vm="${OMNIDECK_VM_E2E_VM:-appimage}"
builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:local}"
assume_yes=0
keep_vm=0
suite="${OMNIDECK_VM_E2E_SUITE:-product}"
original_args=("$@")

usage() {
  cat <<'EOF'
Usage: ./tests/e2e/run.sh [--suite product|onboarding] [--vm appimage|deb|rpm|windows] [--yes] [--keep-vm]

Build and install the current CLI in one clean disposable VM, then drive the
guided install and management TUI through a real pseudo-terminal.

The selected guest must be stopped. It is reset to the lab's clean golden both
before and after the run. --yes accepts that reset noninteractively. --keep-vm
leaves a failed or completed guest stopped for debugging instead of the final
reset; the initial clean reset still occurs.
EOF
}

while (($#)); do
  case "$1" in
    --vm)
      vm="${2:?--vm requires a value}"
      shift 2
      ;;
    --suite)
      suite="${2:?--suite requires a value}"
      shift 2
      ;;
    --yes)
      assume_yes=1
      shift
      ;;
    --keep-vm)
      keep_vm=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$suite" in
  product) profile=product-ready ;;
  onboarding) profile=onboarding-clean ;;
  *) printf 'Unsupported suite: %s\n' "$suite" >&2; exit 2 ;;
esac

if [[ "${vm}" == "windows" ]]; then
  windows_args=(--suite "$suite")
  [[ "${assume_yes}" == "1" ]] && windows_args+=(--yes)
  [[ "${keep_vm}" == "1" ]] && windows_args+=(--keep-vm)
  exec "${script_dir}/run-windows.sh" "${windows_args[@]}"
fi

case "${vm}" in
  appimage|ubuntu) vm=appimage ;;
  deb|debian) vm=deb ;;
  rpm|fedora) vm=rpm ;;
  *)
    printf 'The clean-install E2E suite supports appimage, deb, rpm, or windows; got %q.\n' "${vm}" >&2
    exit 2
    ;;
esac

require_lab
baseline="$("${lab_dir}/lab.sh" profile "$profile" "$vm")"
command -v docker >/dev/null 2>&1 || { printf 'Docker is required for the pinned builder and fixture registry.\n' >&2; exit 2; }
command -v curl >/dev/null 2>&1 || { printf 'curl is required to check the fixture registry.\n' >&2; exit 2; }
command -v ssh >/dev/null 2>&1 || { printf 'ssh is required to run the guest through the lab connection.\n' >&2; exit 2; }

if [[ "${OMNIDECK_VM_LAB_LEASED:-}" != "1" ]]; then
  lease_run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  "${lab_dir}/lab.sh" preflight cli "$profile" --lanes "${vm}" >/dev/null
  source_state
  prepare_output_dir="${OMNIDECK_VM_E2E_OUTPUT_DIR:-$("${lab_dir}/lab.sh" artifact-path cli e2e "${lease_run_id}")}"
  mkdir -p "${prepare_output_dir}"
  "${lab_dir}/lab.sh" evidence-init "${prepare_output_dir}" cli e2e "${lease_run_id}" \
    "${source_short}" "${vm}" "$baseline" "phase=preparing" "testTier=${suite}" "sourceDirty=${source_dirty}" "sourceFingerprint=${source_fingerprint}"
  trap '"${lab_dir}/lab.sh" evidence-finish "${prepare_output_dir}" failed || true' EXIT
  prepare_cli_binaries linux
  "${lab_dir}/lab.sh" evidence-set "${prepare_output_dir}" "phase=prepared" "buildCacheKey=${cli_build_key}"
  lease_args=(lease "${vm}" cli "${lease_run_id}" --cleanup-baseline "$baseline")
  [[ "${keep_vm}" != "1" ]] || lease_args+=(--keep-state)
  lease_args+=(-- env OMNIDECK_CLI_BUILD_CACHE="${cli_build_cache}" OMNIDECK_CLI_BUILD_KEY="${cli_build_key}" \
    OMNIDECK_VM_E2E_OUTPUT_DIR="${prepare_output_dir}" "$0" "${original_args[@]}")
  lease_status=0
  "${lab_dir}/lab.sh" "${lease_args[@]}" || lease_status=$?
  trap - EXIT
  if [[ "${lease_status}" != "0" ]]; then
    "${lab_dir}/lab.sh" evidence-finish "${prepare_output_dir}" failed || true
  fi
  exit "${lease_status}"
fi
eval "$("${lab_dir}/lab.sh" describe "${vm}" --shell)"
ssh_port="${LAB_VM_SSH_PORT}"

status="$("${lab_dir}/lab.sh" status "${vm}")"
printf '%s\n' "${status}"
grep -Eq "^${vm} stopped " <<<"${status}" || {
  printf 'Refusing to use a running guest. Stop it only if you own that VM lane.\n' >&2
  exit 1
}

if [[ "${assume_yes}" != "1" ]]; then
  [[ -t 0 ]] || {
    printf 'This run resets the %s disposable guest. Re-run interactively or pass --yes.\n' "${vm}" >&2
    exit 2
  }
  printf 'This will reset only the stopped %s disposable VM to its clean golden before and after the test.\n' "${vm}"
  printf 'Type %s to continue: ' "${vm}"
  read -r confirmation
  [[ "${confirmation}" == "${vm}" ]] || { printf 'Canceled.\n'; exit 1; }
fi

run_id="${OMNIDECK_VM_LAB_RUN_ID}"
safe_run_id="$(printf '%s' "${run_id}" | tr -cd '[:alnum:]_.-')"
source_state
source_commit="${source_short}"
expected_version="vm-e2e-${source_short}"
output_dir="${OMNIDECK_VM_E2E_OUTPUT_DIR:-$("${lab_dir}/lab.sh" artifact-path cli e2e "${safe_run_id}")}"
build_dir="${output_dir}/build"
remote_root="/tmp/omnideck-cli-e2e-${safe_run_id}"
registry_name="omnideck-vm-e2e-registry-${safe_run_id}"
fixture_local="omnideck-vm-e2e-fixture:${safe_run_id}"
fixture_repository="omnideck-vm-e2e-fixture"
reverse_port="$((47000 + ($$ % 1000)))"
key_file="${LAB_VM_KEY}"
known_hosts="${LAB_VM_KNOWN_HOSTS}"
vm_started=0
registry_started=0
remote_staged=0
test_status=1
fixture_host=""

mkdir -p "${build_dir}"
cp -a "${OMNIDECK_CLI_BUILD_CACHE:?prepared build cache is required}/." "${build_dir}/"
builder_image="$(<"${build_dir}/builder-image.txt")"
write_source_metadata
if [[ -f "${output_dir}/run.json" ]]; then
  "${lab_dir}/lab.sh" evidence-set "${output_dir}" "phase=executing" "expectedVersion=${expected_version}"
else
  "${lab_dir}/lab.sh" evidence-init "${output_dir}" cli e2e "${safe_run_id}" \
    "${source_commit}" "${vm}" "$baseline" "expectedVersion=${expected_version}" "testTier=${suite}" \
    "sourceDirty=${source_dirty}" "sourceFingerprint=${source_fingerprint}" \
    "buildCacheKey=${OMNIDECK_CLI_BUILD_KEY}"
fi

cleanup() {
  local exit_code=$?
  set +e
  if [[ "${remote_staged}" == "1" && "${vm_started}" == "1" && "${keep_vm}" != "1" ]]; then
    "${lab_dir}/lab.sh" run "${vm}" "rm -rf -- '${remote_root}'" >/dev/null 2>&1 || true
  fi
  if [[ "${registry_started}" == "1" ]]; then
    docker rm -f "${registry_name}" >/dev/null 2>&1 || true
  fi
  docker image rm -f "${fixture_local}" >/dev/null 2>&1 || true
  if [[ -n "${fixture_host}" ]]; then
    docker image rm -f "${fixture_host}" >/dev/null 2>&1 || true
  fi
  if [[ "${vm_started}" == "1" ]]; then
    "${lab_dir}/lab.sh" stop "${vm}" || exit_code=1
    vm_started=0
  fi
  if [[ "${keep_vm}" != "1" ]]; then
    "${lab_dir}/lab.sh" reset "${vm}" "$baseline" || exit_code=1
  else
    printf 'Guest kept stopped for debugging: %s\n' "${vm}"
  fi
  if [[ "${exit_code}" == "0" ]]; then
    "${lab_dir}/lab.sh" evidence-finish "${output_dir}" passed || exit_code=1
  else
    "${lab_dir}/lab.sh" evidence-finish "${output_dir}" failed || true
  fi
  printf 'E2E artifacts: %s\n' "${output_dir}"
  exit "${exit_code}"
}
trap cleanup EXIT

printf 'Resetting the leased %s guest to its %s baseline.\n' "${vm}" "$baseline"
"${lab_dir}/lab.sh" reset "${vm}" "$baseline"

printf 'Using prepared CLI build cache: %s\n' "${OMNIDECK_CLI_BUILD_KEY}"

printf 'Starting loopback-only fixture registry.\n'
docker build --tag "${fixture_local}" --file "${repo_root}/tests/hardware/fixture/Containerfile" "${repo_root}/tests/hardware/fixture"
docker run -d --name "${registry_name}" --publish 127.0.0.1::5000 docker.io/library/registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373 >/dev/null
registry_started=1
host_registry_port="$(docker port "${registry_name}" 5000/tcp | awk -F: 'NR == 1 {print $NF}')"
[[ "${host_registry_port}" =~ ^[0-9]+$ ]] || { printf 'Could not resolve the fixture registry port.\n' >&2; exit 1; }
for _ in $(seq 1 30); do
  if curl --fail --silent --max-time 2 "http://127.0.0.1:${host_registry_port}/v2/" >/dev/null; then break; fi
  sleep 1
done
curl --fail --silent --max-time 2 "http://127.0.0.1:${host_registry_port}/v2/" >/dev/null
fixture_host="127.0.0.1:${host_registry_port}/${fixture_repository}:${safe_run_id}"
docker tag "${fixture_local}" "${fixture_host}"
docker push "${fixture_host}" >/dev/null
fixture_guest="localhost:${reverse_port}/${fixture_repository}:${safe_run_id}"

printf 'Starting and verifying the %s %s guest.\n' "$suite" "${vm}"
"${lab_dir}/lab.sh" start "${vm}"
vm_started=1
"${lab_dir}/lab.sh" wait "${vm}"
"${lab_dir}/lab.sh" verify "${vm}" | tee "${output_dir}/guest-verify.txt"
if [[ "$suite" == onboarding ]]; then
  grep -Fq 'podman=absent' "${output_dir}/guest-verify.txt"
else
  grep -Fq 'podman=/' "${output_dir}/guest-verify.txt"
  "${lab_dir}/lab.sh" run "${vm}" 'podman info >/dev/null'
fi
"${lab_dir}/lab.sh" run "${vm}" "if ss -ltn | grep -q ':${reverse_port} '; then exit 1; fi"

payload_dir="${build_dir}/payload"
mkdir -p "${payload_dir}/elevation-bin"
install -m 0644 "${build_dir}/omnideck-linux-amd64.tar.gz" "${payload_dir}/omnideck-linux-amd64.tar.gz"
install -m 0644 "${build_dir}/SHA256SUMS" "${payload_dir}/SHA256SUMS"
install -m 0755 "${build_dir}/releasecontract" "${payload_dir}/releasecontract"
install -m 0644 "${build_dir}/contracts.tar.gz" "${payload_dir}/contracts.tar.gz"
install -m 0755 "${script_dir}/guest.sh" "${payload_dir}/guest.sh"
install -m 0755 "${script_dir}/terminal_driver.py" "${payload_dir}/terminal_driver.py"
install -m 0755 "${script_dir}/fixtures/sudo" "${payload_dir}/elevation-bin/sudo"
install -m 0755 "${repo_root}/tests/hardware/run.sh" "${payload_dir}/hardware-run.sh"
"${lab_dir}/lab.sh" stage "${vm}" "${payload_dir}" "${remote_root}" | tee "${output_dir}/payload-stage.txt"
remote_staged=1

ssh_options=(
  -i "${key_file}"
  -o "UserKnownHostsFile=${known_hosts}"
  -o StrictHostKeyChecking=yes
  -o ConnectTimeout=8
  -o ExitOnForwardFailure=yes
  -p "${ssh_port}"
  -R "${reverse_port}:127.0.0.1:${host_registry_port}"
)

set +e
ssh "${ssh_options[@]}" tester@127.0.0.1 \
  "OMNIDECK_E2E_KEEP_GUEST_STATE='${keep_vm}' '${remote_root}/guest.sh' '${remote_root}' '${expected_version}' '${fixture_guest}' '${suite}'"
test_status=$?
set -e

if "${lab_dir}/lab.sh" copy-from "${vm}" "${remote_root}/evidence.tar.gz" "${output_dir}/evidence.tar.gz"; then
  mkdir -p "${output_dir}/evidence"
  tar -xzf "${output_dir}/evidence.tar.gz" -C "${output_dir}/evidence"
fi

exit "${test_status}"
