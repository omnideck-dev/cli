#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
lab_dir="${OMNIDECK_VM_LAB_DIR:-}"
vm="${OMNIDECK_VM_E2E_VM:-appimage}"
builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:local}"
assume_yes=0
keep_vm=0
original_args=("$@")

usage() {
  cat <<'EOF'
Usage: ./tests/e2e/run.sh [--vm appimage|deb|rpm|windows] [--yes] [--keep-vm]

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

if [[ "${vm}" == "windows" ]]; then
  windows_args=()
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

[[ -n "${lab_dir}" ]] || { printf 'Set OMNIDECK_VM_LAB_DIR to the external VM lab root.\n' >&2; exit 2; }
[[ -x "${lab_dir}/lab.sh" ]] || { printf 'Missing executable lab.sh under %s\n' "${lab_dir}" >&2; exit 2; }
lab_dir="$(cd "${lab_dir}" && pwd -P)"
[[ "$("${lab_dir}/lab.sh" --version 2>/dev/null || true)" == "omnideck-vm-lab 2."* ]] || {
  printf 'CLI VM E2E requires OmniDeck VM lab controller 2.x.\n' >&2
  exit 2
}
command -v docker >/dev/null 2>&1 || { printf 'Docker is required for the pinned builder and fixture registry.\n' >&2; exit 2; }
command -v curl >/dev/null 2>&1 || { printf 'curl is required to check the fixture registry.\n' >&2; exit 2; }
command -v ssh >/dev/null 2>&1 || { printf 'ssh is required to run the guest through the lab connection.\n' >&2; exit 2; }

if [[ "${OMNIDECK_VM_LAB_LEASED:-}" != "1" ]]; then
  lease_run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  lease_args=(lease "${vm}" cli "${lease_run_id}")
  [[ "${keep_vm}" != "1" ]] || lease_args+=(--keep-state)
  lease_args+=(-- "$0" "${original_args[@]}")
  exec "${lab_dir}/lab.sh" "${lease_args[@]}"
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
source_commit="$(git -C "${repo_root}" rev-parse --short=12 HEAD)"
expected_version="vm-e2e-${source_commit}"
output_dir="${OMNIDECK_VM_E2E_OUTPUT_DIR:-${lab_dir}/artifacts/cli/e2e/${safe_run_id}}"
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
"${lab_dir}/lab.sh" evidence-init "${output_dir}" cli e2e "${safe_run_id}" \
  "${source_commit}" "${vm}" clean "expectedVersion=${expected_version}"

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
    "${lab_dir}/lab.sh" reset "${vm}" clean || exit_code=1
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

printf 'Resetting the leased %s guest to its clean golden.\n' "${vm}"
"${lab_dir}/lab.sh" reset "${vm}" clean

if ! docker image inspect "${builder_image}" >/dev/null 2>&1; then
  printf 'Building the pinned Go builder image: %s\n' "${builder_image}"
  docker build --tag "${builder_image}" --file "${repo_root}/.devcontainer/Dockerfile" "${repo_root}/.devcontainer"
fi
docker image inspect "${builder_image}" --format '{{.Id}}' > "${build_dir}/builder-image.txt"

printf 'Building release-shaped CLI archive for %s.\n' "${source_commit}"
docker run --rm --entrypoint /bin/zsh \
  --user "$(id -u):$(id -g)" \
  --env GOCACHE=/tmp/omnideck-go-build \
  --env GOPATH=/tmp/omnideck-go \
  -v "${repo_root}:/workspace:ro" \
  -v "${build_dir}:/out" \
  -w /workspace "${builder_image}" \
  -c "go build -trimpath -buildvcs=false -ldflags '-X main.version=${expected_version} -X main.commit=${source_commit} -X main.date=vm-e2e' -o /out/omnideck . && go build -trimpath -buildvcs=false -o /out/releasecontract ./tests/releasecontract"
chmod +x "${build_dir}/omnideck"
chmod +x "${build_dir}/releasecontract"
tar --sort=name --owner=0 --group=0 --numeric-owner -czf "${build_dir}/omnideck-linux-amd64.tar.gz" -C "${build_dir}" omnideck
tar --sort=name --owner=0 --group=0 --numeric-owner -czf "${build_dir}/contracts.tar.gz" -C "${repo_root}" contracts
(cd "${build_dir}" && sha256sum omnideck-linux-amd64.tar.gz > SHA256SUMS)
sha256sum "${build_dir}/omnideck" > "${build_dir}/omnideck.sha256"

printf 'Starting loopback-only fixture registry.\n'
docker build --tag "${fixture_local}" --file "${repo_root}/tests/hardware/fixture/Containerfile" "${repo_root}/tests/hardware/fixture"
docker run -d --name "${registry_name}" --publish 127.0.0.1::5000 docker.io/library/registry:2.8.3 >/dev/null
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

printf 'Starting and verifying the clean %s guest.\n' "${vm}"
"${lab_dir}/lab.sh" start "${vm}"
vm_started=1
"${lab_dir}/lab.sh" wait "${vm}"
"${lab_dir}/lab.sh" verify "${vm}" | tee "${output_dir}/guest-verify.txt"
grep -Fq 'podman=absent' "${output_dir}/guest-verify.txt"
"${lab_dir}/lab.sh" run "${vm}" "if ss -ltn | grep -q ':${reverse_port} '; then exit 1; fi"

"${lab_dir}/lab.sh" run "${vm}" "mkdir -p '${remote_root}/elevation-bin'"
remote_staged=1
"${lab_dir}/lab.sh" copy-to "${vm}" "${build_dir}/omnideck-linux-amd64.tar.gz" "${remote_root}/omnideck-linux-amd64.tar.gz"
"${lab_dir}/lab.sh" copy-to "${vm}" "${build_dir}/SHA256SUMS" "${remote_root}/SHA256SUMS"
"${lab_dir}/lab.sh" copy-to "${vm}" "${build_dir}/releasecontract" "${remote_root}/releasecontract"
"${lab_dir}/lab.sh" copy-to "${vm}" "${build_dir}/contracts.tar.gz" "${remote_root}/contracts.tar.gz"
"${lab_dir}/lab.sh" copy-to "${vm}" "${script_dir}/guest.sh" "${remote_root}/guest.sh"
"${lab_dir}/lab.sh" copy-to "${vm}" "${script_dir}/terminal_driver.py" "${remote_root}/terminal_driver.py"
"${lab_dir}/lab.sh" copy-to "${vm}" "${script_dir}/fixtures/sudo" "${remote_root}/elevation-bin/sudo"
"${lab_dir}/lab.sh" copy-to "${vm}" "${repo_root}/tests/hardware/run.sh" "${remote_root}/hardware-run.sh"

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
  "chmod +x '${remote_root}/guest.sh' '${remote_root}/terminal_driver.py' '${remote_root}/releasecontract' '${remote_root}/hardware-run.sh' '${remote_root}/elevation-bin/sudo' && OMNIDECK_E2E_KEEP_GUEST_STATE='${keep_vm}' '${remote_root}/guest.sh' '${remote_root}' '${expected_version}' '${fixture_guest}'"
test_status=$?
set -e

if "${lab_dir}/lab.sh" copy-from "${vm}" "${remote_root}/evidence.tar.gz" "${output_dir}/evidence.tar.gz"; then
  mkdir -p "${output_dir}/evidence"
  tar -xzf "${output_dir}/evidence.tar.gz" -C "${output_dir}/evidence"
fi

exit "${test_status}"
