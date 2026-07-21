#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
host_os="$(uname -s)"
host_arch="$(uname -m)"
requested_engine="${OMNIDECK_HARDWARE_ENGINE:-auto}"
provided_cli="${OMNIDECK_HARDWARE_CLI:-}"
keep_resources="${OMNIDECK_HARDWARE_KEEP_RESOURCES:-0}"
run_id="${GITHUB_RUN_ID:-local}-$(date -u +%Y%m%dT%H%M%SZ)-$$"
safe_run_id="$(printf '%s' "${run_id}" | tr -cd '[:alnum:]._-')"
instance="omnideck-hw-${safe_run_id}"
port="${OMNIDECK_HARDWARE_PORT:-$((42000 + ($$ % 2000)))}"
registry_port="${OMNIDECK_HARDWARE_REGISTRY_PORT:-$((46000 + ($$ % 1000)))}"
output_dir="${OMNIDECK_HARDWARE_OUTPUT_DIR:-${repo_root}/artifacts/hardware/${host_os}-${host_arch}-${safe_run_id}}"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/omnideck-hardware.XXXXXX")"
export OMNIDECK_CONFIG_DIR="${temp_dir}/config"
cli_path="${temp_dir}/omnideck"
log_file="${output_dir}/hardware-test.log"
summary_file="${output_dir}/summary.json"
junit_file="${output_dir}/junit.xml"
config_path=""
engine=""
fixture_image="${OMNIDECK_HARDWARE_TEST_IMAGE:-}"
local_fixture_image=""
registry_container="${instance}-registry"
built_fixture=0
current_step="initialization"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

mkdir -p "${output_dir}"
exec > >(tee -a "${log_file}") 2>&1

fail() {
  printf 'ERROR: %s\n' "$*"
  return 1
}

runtime_ready() {
  command -v "$1" >/dev/null 2>&1 && "$1" info >/dev/null 2>&1
}

select_engine() {
  if [[ "${requested_engine}" != "auto" ]]; then
    engine="${requested_engine}"
    return
  fi

  case "${host_os}/${host_arch}" in
    Darwin/arm64)
      if runtime_ready podman; then engine=podman; else engine=docker; fi
      ;;
    Darwin/*)
      if runtime_ready docker; then engine=docker; else engine=podman; fi
      ;;
    Linux/*)
      if runtime_ready podman; then engine=podman; else engine=docker; fi
      ;;
    *)
      if runtime_ready docker; then engine=docker; else engine=podman; fi
      ;;
  esac
}

cleanup() {
  [[ "${instance}" == omnideck-hw-* ]] || return 0
  if [[ -n "${engine}" ]] && command -v "${engine}" >/dev/null 2>&1; then
    "${engine}" rm -f "${instance}" >/dev/null 2>&1 || true
    "${engine}" rm -f "${registry_container}" >/dev/null 2>&1 || true
    "${engine}" volume rm "${instance}-home" "${instance}-state" >/dev/null 2>&1 || true
    if [[ "${built_fixture}" == "1" && -n "${fixture_image}" ]]; then
      "${engine}" rmi -f "${fixture_image}" >/dev/null 2>&1 || true
      "${engine}" rmi -f "${local_fixture_image}" >/dev/null 2>&1 || true
    fi
  fi
  if [[ -n "${config_path}" && "$(basename "${config_path}")" == "${instance}.yaml" ]]; then
    rm -f -- "${config_path}"
  fi
}

write_results() {
  local exit_code="$1"
  local status="passed"
  if [[ "${exit_code}" != "0" ]]; then status="failed"; fi

  printf '{\n  "status": "%s",\n  "last_step": "%s",\n  "platform": "%s",\n  "architecture": "%s",\n  "engine": "%s",\n  "instance": "%s",\n  "started_at": "%s",\n  "finished_at": "%s"\n}\n' \
    "${status}" "${current_step}" "${host_os}" "${host_arch}" "${engine}" "${instance}" \
    "${started_at}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${summary_file}"

  if [[ "${status}" == "passed" ]]; then
    printf '<?xml version="1.0" encoding="UTF-8"?>\n<testsuite name="omnideck-hardware" tests="1" failures="0"><testcase classname="hardware.%s" name="lifecycle-%s"/></testsuite>\n' \
      "${host_os}" "${engine}" > "${junit_file}"
  else
    printf '<?xml version="1.0" encoding="UTF-8"?>\n<testsuite name="omnideck-hardware" tests="1" failures="1"><testcase classname="hardware.%s" name="lifecycle-%s"><failure message="See hardware-test.log; failed during %s"/></testcase></testsuite>\n' \
      "${host_os}" "${engine}" "${current_step}" > "${junit_file}"
  fi
}

on_exit() {
  local exit_code=$?
  set +e
  if [[ "${keep_resources}" != "1" ]]; then cleanup; fi
  write_results "${exit_code}"
  rm -r -- "${temp_dir}"
  trap - EXIT
  exit "${exit_code}"
}
trap on_exit EXIT

run_cli() {
  "${cli_path}" --no-color --name "${instance}" "$@"
}

wait_for_web_ui() {
  local attempt
  for ((attempt = 1; attempt <= 45; attempt++)); do
    if curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${port}" > "${output_dir}/web-ui.html"; then
      grep -Fq "omnideck hardware fixture ready" "${output_dir}/web-ui.html"
      return
    fi
    sleep 2
  done
  fail "The fixture web UI did not become ready on port ${port}."
}

wait_for_registry() {
  local attempt
  for ((attempt = 1; attempt <= 30; attempt++)); do
    if curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${registry_port}/v2/" >/dev/null; then
      return
    fi
    sleep 1
  done
  fail "The temporary image registry did not become ready on port ${registry_port}."
}

printf 'Omnideck hardware lifecycle test\n'
printf 'Platform: %s/%s\n' "${host_os}" "${host_arch}"
printf 'Artifacts: %s\n' "${output_dir}"

if [[ ! "${port}" =~ ^[0-9]+$ ]] || (( port < 1024 || port > 65535 )); then
  fail "OMNIDECK_HARDWARE_PORT must be a number from 1024 through 65535."
fi
if [[ ! "${registry_port}" =~ ^[0-9]+$ ]] || (( registry_port < 1024 || registry_port > 65535 )); then
  fail "OMNIDECK_HARDWARE_REGISTRY_PORT must be a number from 1024 through 65535."
fi
[[ "${port}" != "${registry_port}" ]] || fail "The web UI and temporary registry ports must be different."
if [[ -z "${provided_cli}" ]]; then
  command -v go >/dev/null 2>&1 || fail "Go is required to build the CLI. Set OMNIDECK_HARDWARE_CLI to test a prebuilt binary instead."
fi
command -v curl >/dev/null 2>&1 || fail "curl is required to verify the fixture web UI."

select_engine
if [[ "${engine}" != "docker" && "${engine}" != "podman" ]]; then
  fail "OMNIDECK_HARDWARE_ENGINE must be auto, docker, or podman."
fi
runtime_ready "${engine}" || fail "${engine} is not installed and ready. Start it, or choose another engine."

if [[ "${engine}" == "podman" && -z "${fixture_image}" ]]; then
  podman_registry_config="${temp_dir}/registries.conf"
  printf '[[registry]]\nlocation = "localhost:%s"\ninsecure = true\n' "${registry_port}" > "${podman_registry_config}"
  export CONTAINERS_REGISTRIES_CONF="${podman_registry_config}"
fi

current_step="record runtime information"
"${engine}" version

current_step="build CLI"
if [[ -n "${provided_cli}" ]]; then
  [[ -x "${provided_cli}" ]] || fail "OMNIDECK_HARDWARE_CLI must point to an executable CLI binary."
  cli_path="$(cd "$(dirname "${provided_cli}")" && pwd)/$(basename "${provided_cli}")"
else
  (cd "${repo_root}" && go build -o "${cli_path}" .)
fi
run_cli --version
run_cli --help >/dev/null

config_path="$(run_cli config path | tail -n 1 | tr -d '\r')"
[[ -n "${config_path}" ]] || fail "The CLI did not report a configuration path."

if "${engine}" container inspect "${instance}" >/dev/null 2>&1 || [[ -e "${config_path}" ]]; then
  fail "The generated test instance already exists: ${instance}"
fi
if curl --fail --silent --max-time 1 "http://127.0.0.1:${port}" >/dev/null 2>&1; then
  fail "Port ${port} is already in use. Set OMNIDECK_HARDWARE_PORT to an unused port."
fi

current_step="build fixture image"
if [[ -z "${fixture_image}" ]]; then
  local_fixture_image="localhost/omnideck-hardware-fixture:${safe_run_id}"
  fixture_image="localhost:${registry_port}/omnideck-hardware-fixture:${safe_run_id}"
  "${engine}" build --file "${script_dir}/fixture/Containerfile" --tag "${local_fixture_image}" "${script_dir}/fixture"
  built_fixture=1
  "${engine}" run -d --name "${registry_container}" -p "127.0.0.1:${registry_port}:5000" docker.io/library/registry:2.8.3
  wait_for_registry
  "${engine}" tag "${local_fixture_image}" "${fixture_image}"
  "${engine}" push "${fixture_image}"
fi

current_step="setup"
run_cli setup --plain --runtime "${engine}" --image "${fixture_image}" \
  --port "${port}" --memory 512m --shm-size 64m

settings_path="${OMNIDECK_CONFIG_DIR}/settings.yaml"
grep -Eq "^runtime:[[:space:]]+${engine}$" "${settings_path}" || fail "The shared settings did not record runtime: ${engine}."
if grep -Eq "^engine:" "${config_path}"; then fail "The instance configuration still contains a per-instance runtime."; fi
grep -Eq "^container_name:[[:space:]]+${instance}$" "${config_path}" || fail "The saved configuration has the wrong container name."

current_step="verify web UI"
wait_for_web_ui

current_step="status"
run_cli status

current_step="logs"
run_cli logs --follow=false --tail 20 | tee "${output_dir}/container.log"
grep -Fq "omnideck-hardware-fixture-started" "${output_dir}/container.log" || fail "Expected fixture startup log was not returned."

current_step="configuration"
run_cli config show | tee "${output_dir}/config-show.log"

current_step="volume persistence"
"${engine}" exec "${instance}" sh -c 'printf "%s\n" hardware-volume-marker > /home/omnideck/hardware-marker'

current_step="stop"
run_cli stop
set +e
run_cli status > "${output_dir}/status-while-stopped.log" 2>&1
stopped_status=$?
set -e
[[ "${stopped_status}" != "0" ]] || fail "status succeeded even though the container was stopped."
cat "${output_dir}/status-while-stopped.log"

current_step="start"
run_cli start
wait_for_web_ui
marker="$("${engine}" exec "${instance}" cat /home/omnideck/hardware-marker | tr -d '\r')"
[[ "${marker}" == "hardware-volume-marker" ]] || fail "The home volume marker did not survive a stop and start."

current_step="restart"
run_cli restart
wait_for_web_ui
run_cli status

current_step="doctor"
run_cli doctor | tee "${output_dir}/doctor.log"
grep -Fq "Omnideck Doctor Report" "${output_dir}/doctor.log" || fail "doctor did not render its report."

current_step="uninstall"
printf 'yes\nyes\nno\n' | run_cli uninstall

current_step="verify cleanup"
if "${engine}" container inspect "${instance}" >/dev/null 2>&1; then
  fail "The container still exists after uninstall."
fi
if "${engine}" volume inspect "${instance}-home" >/dev/null 2>&1; then
  fail "The home volume still exists after uninstall."
fi
if "${engine}" volume inspect "${instance}-state" >/dev/null 2>&1; then
  fail "The state volume still exists after uninstall."
fi
[[ ! -e "${config_path}" ]] || fail "The configuration still exists after uninstall."

current_step="complete"
printf '\nPASS: lifecycle completed with %s on %s/%s.\n' "${engine}" "${host_os}" "${host_arch}"
