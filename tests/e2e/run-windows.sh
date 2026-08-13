#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
source "${script_dir}/_common.sh"
builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:local}"
assume_yes=0
keep_vm=0
vm=windows
original_args=("$@")

usage() {
  cat <<'EOF'
Usage: ./tests/e2e/run-windows.sh [--yes] [--keep-vm]

Build the release-shaped Windows CLI ZIP, reset and boot only the disposable
Windows lab guest, approve its real UAC prompt, exercise the required reboot,
then drive attended TUI and unattended CLI behavior through the local lab.
EOF
}

while (($#)); do
  case "$1" in
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

require_lab
for dependency in docker curl ssh python3 openssl socat zip unzip; do
  command -v "${dependency}" >/dev/null 2>&1 || { printf '%s is required by the Windows VM E2E lane.\n' "${dependency}" >&2; exit 2; }
done

if [[ "${OMNIDECK_VM_LAB_LEASED:-}" != "1" ]]; then
  lease_run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  "${lab_dir}/lab.sh" preflight cli release-clean --lanes windows >/dev/null
  source_state
  prepare_output_dir="${OMNIDECK_VM_E2E_OUTPUT_DIR:-$("${lab_dir}/lab.sh" artifact-path cli e2e "${lease_run_id}")}"
  mkdir -p "${prepare_output_dir}"
  "${lab_dir}/lab.sh" evidence-init "${prepare_output_dir}" cli e2e "${lease_run_id}" \
    "${source_short}" windows clean "phase=preparing" "sourceDirty=${source_dirty}" "sourceFingerprint=${source_fingerprint}"
  trap '"${lab_dir}/lab.sh" evidence-finish "${prepare_output_dir}" failed || true' EXIT
  prepare_cli_binaries windows
  "${lab_dir}/lab.sh" evidence-set "${prepare_output_dir}" "phase=prepared" "buildCacheKey=${cli_build_key}"
  lease_args=(lease windows cli "${lease_run_id}" --cleanup-baseline clean)
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
eval "$("${lab_dir}/lab.sh" describe windows --shell)"
ssh_port="${LAB_VM_SSH_PORT}"

status="$("${lab_dir}/lab.sh" status windows)"
printf '%s\n' "${status}"
grep -Eq '^windows stopped ' <<<"${status}" || {
  printf 'Refusing to use a running guest. Stop it only if you own the Windows VM lane.\n' >&2
  exit 1
}

if [[ "${assume_yes}" != "1" ]]; then
  [[ -t 0 ]] || {
    printf 'This run resets the Windows disposable guest. Re-run interactively or pass --yes.\n' >&2
    exit 2
  }
  printf 'This will reset only the stopped Windows disposable VM to its clean golden before and after the test.\n'
  printf 'Type windows to continue: '
  read -r confirmation
  [[ "${confirmation}" == "windows" ]] || { printf 'Canceled.\n'; exit 1; }
fi

run_id="${OMNIDECK_VM_LAB_RUN_ID}"
safe_run_id="$(printf '%s' "${run_id}" | tr -cd '[:alnum:]_.-')"
source_state
source_commit="${source_short}"
expected_version="vm-e2e-${source_short}"
output_dir="${OMNIDECK_VM_E2E_OUTPUT_DIR:-$("${lab_dir}/lab.sh" artifact-path cli e2e "${safe_run_id}")}"
build_dir="${output_dir}/build"
evidence_dir="${output_dir}/evidence"
remote_root="C:\\OmnideckE2E\\${safe_run_id}"
remote_scp_root="C:/OmnideckE2E/${safe_run_id}"
registry_name="omnideck-vm-e2e-registry-${safe_run_id}"
fixture_local="omnideck-vm-e2e-fixture:${safe_run_id}"
fixture_repository="omnideck-vm-e2e-fixture"
tls_port="$((48000 + ($$ % 800)))"
reverse_port="$((49000 + ($$ % 800)))"
bridge_port="$((50000 + ($$ % 800)))"
firewall_rule="OmnideckE2E-${safe_run_id}"
registry_authority="host.containers.internal:${bridge_port}"
fixture_guest="${registry_authority}/${fixture_repository}:${safe_run_id}"
key_file="${LAB_VM_KEY}"
known_hosts="${LAB_VM_KNOWN_HOSTS}"
vm_started=0
initial_reset=0
registry_started=0
remote_staged=0
tls_pid=""
fixture_host=""

mkdir -p "${build_dir}" "${evidence_dir}" "${build_dir}/driver-config"
cp -a "${OMNIDECK_CLI_BUILD_CACHE:?prepared build cache is required}/." "${build_dir}/"
builder_image="$(<"${build_dir}/builder-image.txt")"
write_source_metadata
: > "${build_dir}/registries.conf"
if [[ -f "${output_dir}/run.json" ]]; then
  "${lab_dir}/lab.sh" evidence-set "${output_dir}" "phase=executing" "expectedVersion=${expected_version}" "fixtureImage=${fixture_guest}"
else
  "${lab_dir}/lab.sh" evidence-init "${output_dir}" cli e2e "${safe_run_id}" \
    "${source_commit}" windows clean "expectedVersion=${expected_version}" "fixtureImage=${fixture_guest}" \
    "sourceDirty=${source_dirty}" "sourceFingerprint=${source_fingerprint}" \
    "buildCacheKey=${OMNIDECK_CLI_BUILD_KEY}"
fi

cleanup() {
  local exit_code=$?
  set +e
  if [[ "${vm_started}" == "1" ]]; then
    "${lab_dir}/lab.sh" run windows \
      "netsh.exe interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=${bridge_port}" \
      >/dev/null 2>&1 || true
    "${lab_dir}/lab.sh" run windows \
      "netsh.exe advfirewall firewall delete rule name=${firewall_rule}" \
      >/dev/null 2>&1 || true
    if [[ "${remote_staged}" == "1" && "${keep_vm}" != "1" ]]; then
      "${lab_dir}/lab.sh" run windows \
        "powershell.exe -NoLogo -NoProfile -NonInteractive -Command Remove-Item -Recurse -Force -ErrorAction SilentlyContinue ${remote_root}" \
        >/dev/null 2>&1 || true
    fi
  fi
  if [[ -n "${tls_pid}" ]] && kill -0 "${tls_pid}" 2>/dev/null; then
    kill "${tls_pid}" 2>/dev/null || true
    wait "${tls_pid}" 2>/dev/null || true
  fi
  if [[ "${registry_started}" == "1" ]]; then
    docker rm -f "${registry_name}" >/dev/null 2>&1 || true
  fi
  docker image rm -f "${fixture_local}" >/dev/null 2>&1 || true
  if [[ -n "${fixture_host}" ]]; then
    docker image rm -f "${fixture_host}" >/dev/null 2>&1 || true
  fi
  if [[ "${vm_started}" == "1" ]]; then
    "${lab_dir}/lab.sh" stop windows || exit_code=1
    vm_started=0
  fi
  if [[ "${initial_reset}" == "1" && "${keep_vm}" != "1" ]]; then
    "${lab_dir}/lab.sh" reset windows clean || exit_code=1
  elif [[ "${keep_vm}" == "1" ]]; then
    printf 'Windows guest kept stopped for debugging.\n'
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

printf 'Resetting the leased Windows guest to its clean golden.\n'
"${lab_dir}/lab.sh" reset windows clean
initial_reset=1

printf 'Using prepared CLI build cache: %s\n' "${OMNIDECK_CLI_BUILD_KEY}"

printf 'Starting a loopback-only TLS fixture registry.\n'
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

mkdir -p "${build_dir}/tls"
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 2 \
  -subj '/CN=host.containers.internal' \
  -addext 'subjectAltName=DNS:host.containers.internal' \
  -keyout "${build_dir}/tls/registry.key" \
  -out "${build_dir}/tls/registry.crt" >/dev/null 2>&1
socat \
  "OPENSSL-LISTEN:${tls_port},bind=127.0.0.1,reuseaddr,fork,cert=${build_dir}/tls/registry.crt,key=${build_dir}/tls/registry.key,verify=0" \
  "TCP:127.0.0.1:${host_registry_port}" \
  > "${build_dir}/tls/socat.log" 2>&1 &
tls_pid=$!
for _ in $(seq 1 30); do
  if curl --fail --silent --max-time 2 --cacert "${build_dir}/tls/registry.crt" \
    --resolve "host.containers.internal:${tls_port}:127.0.0.1" \
    "https://host.containers.internal:${tls_port}/v2/" >/dev/null; then break; fi
  sleep 1
done
curl --fail --silent --max-time 2 --cacert "${build_dir}/tls/registry.crt" \
  --resolve "host.containers.internal:${tls_port}:127.0.0.1" \
  "https://host.containers.internal:${tls_port}/v2/" >/dev/null

printf 'Starting and verifying the clean Windows guest.\n'
"${lab_dir}/lab.sh" start windows
vm_started=1
"${lab_dir}/lab.sh" wait windows
"${lab_dir}/lab.sh" verify windows | tee "${output_dir}/guest-verify-before.txt"
grep -Fq 'podman=absent' "${output_dir}/guest-verify-before.txt"

"${lab_dir}/lab.sh" run windows "cmd.exe /d /c if not exist ${remote_root} mkdir ${remote_root}"
remote_staged=1
"${lab_dir}/lab.sh" copy-to windows "${build_dir}/omnideck-windows-amd64.zip" "${remote_scp_root}/omnideck-windows-amd64.zip"
"${lab_dir}/lab.sh" copy-to windows "${build_dir}/SHA256SUMS" "${remote_scp_root}/SHA256SUMS"
"${lab_dir}/lab.sh" copy-to windows "${build_dir}/releasecontract.exe" "${remote_scp_root}/releasecontract.exe"
"${lab_dir}/lab.sh" copy-to windows "${build_dir}/contracts.tar.gz" "${remote_scp_root}/contracts.tar.gz"
"${lab_dir}/lab.sh" copy-to windows "${script_dir}/windows_guest.ps1" "${remote_scp_root}/windows_guest.ps1"
"${lab_dir}/lab.sh" copy-to windows "${script_dir}/windows_registry.ps1" "${remote_scp_root}/windows_registry.ps1"
"${lab_dir}/lab.sh" copy-to windows "${repo_root}/tests/hardware/run.ps1" "${remote_scp_root}/hardware-run.ps1"
"${lab_dir}/lab.sh" copy-to windows "${build_dir}/tls/registry.crt" "${remote_scp_root}/registry.crt"

ssh_base_options=(
  -i "${key_file}"
  -o "UserKnownHostsFile=${known_hosts}"
  -o StrictHostKeyChecking=yes
  -o BatchMode=yes
  -o ConnectTimeout=8
  -o ExitOnForwardFailure=yes
  -p "${ssh_port}"
)
ssh_terminal_options=(
  "${ssh_base_options[@]}"
  -tt
  -R "${reverse_port}:127.0.0.1:${tls_port}"
)

json_command() {
  python3 -c 'import json, sys; print(json.dumps(sys.argv[1:]))' "$@"
}

prepare_command="powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File ${remote_root}\\windows_guest.ps1 -Phase Prepare -WorkDir ${remote_root} -ExpectedVersion ${expected_version} -FixtureImage ${fixture_guest}"
bootstrap_remote="set \"OMNIDECK_CONFIG_DIR=${remote_root}\\config\"&& ${remote_root}\\bin\\omnideck.exe install --image ${fixture_guest}"
install_remote="set \"OMNIDECK_CONFIG_DIR=${remote_root}\\config\"&& ${remote_root}\\bin\\omnideck.exe install --image ${fixture_guest}"
ca_command="powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File ${remote_root}\\windows_registry.ps1 -CertificatePath ${remote_root}\\registry.crt -RegistryAuthority ${registry_authority}"
installed_command="wsl.exe --shutdown&& podman.exe machine start omnideck-runtime&& podman.exe start omnideck&& powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File ${remote_root}\\windows_guest.ps1 -Phase Installed -WorkDir ${remote_root} -ExpectedVersion ${expected_version} -FixtureImage ${fixture_guest}&& echo OMNIDECK_E2E_INSTALLED_PASSED"
manage_remote="wsl.exe --shutdown&& podman.exe machine start omnideck-runtime&& podman.exe start omnideck&& set \"OMNIDECK_CONFIG_DIR=${remote_root}\\config\"&& ${remote_root}\\bin\\omnideck.exe tui"
final_command="wsl.exe --shutdown&& podman.exe machine start omnideck-runtime&& powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File ${remote_root}\\windows_guest.ps1 -Phase Final -WorkDir ${remote_root} -ExpectedVersion ${expected_version} -FixtureImage ${fixture_guest}&& echo OMNIDECK_E2E_FINAL_PASSED"

set_registry_bridge() {
	local target_port="$1"
	"${lab_dir}/lab.sh" run windows \
		"netsh.exe interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=${bridge_port}" \
		>/dev/null 2>&1 || true
	"${lab_dir}/lab.sh" run windows \
		"netsh.exe interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=${bridge_port} connectaddress=127.0.0.1 connectport=${target_port}"
}

set +e
(
  set -Eeuo pipefail
  printf 'Validating the packaged CLI on the clean Windows host.\n'
  "${lab_dir}/lab.sh" run windows "${prepare_command}"

	printf 'Bridging the fixture registry into the Podman machine.\n'
	set_registry_bridge "${reverse_port}"
  "${lab_dir}/lab.sh" run windows \
    "netsh.exe advfirewall firewall add rule name=${firewall_rule} dir=in action=allow protocol=TCP localport=${bridge_port} profile=any"

  printf 'Driving Windows prerequisite setup and approving the real UAC prompt.\n'
  bootstrap_json="$(json_command ssh "${ssh_terminal_options[@]}" tester@127.0.0.1 "${bootstrap_remote}")"
  uac_hook_json="$(json_command "${script_dir}/windows_uac_hook.sh" "${lab_dir}" "${evidence_dir}")"
  python3 "${script_dir}/terminal_driver.py" windows-bootstrap \
    --binary /dev/null \
    --config-dir "${build_dir}/driver-config" \
    --registries-conf "${build_dir}/registries.conf" \
    --fixture-image "${fixture_guest}" \
    --artifact-dir "${evidence_dir}" \
    --command-json "${bootstrap_json}" \
    --hook-command-json "${uac_hook_json}"

  printf 'Restarting Windows and waiting for a complete SSH disconnect/reconnect cycle.\n'
  "${lab_dir}/lab.sh" run windows "shutdown.exe /r /t 0 /f" >/dev/null 2>&1 || true
  observed_disconnect=0
  for _ in $(seq 1 90); do
    if ! ssh "${ssh_base_options[@]}" tester@127.0.0.1 exit >/dev/null 2>&1; then
      observed_disconnect=1
      break
    fi
    sleep 1
  done
  [[ "${observed_disconnect}" == "1" ]] || { printf 'Windows never disconnected for its required restart.\n' >&2; exit 1; }
  "${lab_dir}/lab.sh" wait windows
  "${lab_dir}/lab.sh" verify windows | tee "${output_dir}/guest-verify-after-restart.txt"
  grep -Fq 'podman=absent' "${output_dir}/guest-verify-after-restart.txt"

  printf 'Driving the post-restart Podman install and first Omnideck instance.\n'
  install_json="$(json_command ssh "${ssh_terminal_options[@]}" tester@127.0.0.1 "${install_remote}")"
  ca_hook_json="$(json_command "${lab_dir}/lab.sh" run windows "${ca_command}")"
  python3 "${script_dir}/terminal_driver.py" windows-install \
    --binary /dev/null \
    --config-dir "${build_dir}/driver-config" \
    --registries-conf "${build_dir}/registries.conf" \
    --fixture-image "${fixture_guest}" \
    --artifact-dir "${evidence_dir}" \
    --install-timeout 2400 \
    --command-json "${install_json}" \
    --hook-command-json "${ca_hook_json}"

  printf 'Checking installed state and unattended JSON/update behavior.\n'
  # Windows OpenSSH places session children in a job that is torn down when
  # the session closes. Start the WSL-backed Podman machine inside every phase
  # that uses it, and attach the registry forward to that same live session.
  ssh "${ssh_terminal_options[@]}" tester@127.0.0.1 "${installed_command}" \
    | tee "${build_dir}/windows-installed-session.log"
  grep -Fq 'OMNIDECK_E2E_INSTALLED_PASSED' "${build_dir}/windows-installed-session.log"

  printf 'Driving the full returning-user management TUI.\n'
  manage_json="$(json_command ssh "${ssh_terminal_options[@]}" tester@127.0.0.1 "${manage_remote}")"
  python3 "${script_dir}/terminal_driver.py" manage \
    --binary /dev/null \
    --config-dir "${build_dir}/driver-config" \
    --registries-conf "${build_dir}/registries.conf" \
    --fixture-image "${fixture_guest}" \
    --artifact-dir "${evidence_dir}" \
    --expected-log-count 2 \
    --command-json "${manage_json}"

  printf 'Running the full unattended Windows CLI lifecycle.\n'
  ssh "${ssh_terminal_options[@]}" tester@127.0.0.1 "${final_command}" \
    | tee "${build_dir}/windows-final-session.log"
  grep -Fq 'OMNIDECK_E2E_FINAL_PASSED' "${build_dir}/windows-final-session.log"
)
test_status=$?
set -e

if [[ "${vm_started}" == "1" ]]; then
  "${lab_dir}/lab.sh" run windows \
    "powershell.exe -NoLogo -NoProfile -NonInteractive -Command if (-not (Test-Path ${remote_root}\\evidence.zip)) { Compress-Archive -Force -Path ${remote_root}\\results\\* -DestinationPath ${remote_root}\\evidence.zip }" \
    >/dev/null 2>&1 || true
  "${lab_dir}/lab.sh" copy-from windows "${remote_scp_root}/evidence.zip" "${output_dir}/evidence.zip"
  unzip_status=0
  unzip -q -o "${output_dir}/evidence.zip" -d "${evidence_dir}" || unzip_status=$?
  case "${unzip_status}" in
    0|1) ;;
    *) printf 'Could not extract Windows evidence (unzip exit %s).\n' "${unzip_status}" >&2; exit "${unzip_status}" ;;
  esac
	if [[ "${test_status}" == "0" && -f "${evidence_dir}/summary.json" ]]; then
		python3 - "${evidence_dir}/summary.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8-sig") as stream:
    summary = json.load(stream)
assert summary["status"] == "passed", summary
PY
	elif [[ "${test_status}" == "0" ]]; then
		printf 'Windows E2E completed without its required summary.json evidence.\n' >&2
		exit 1
	fi
fi

exit "${test_status}"
