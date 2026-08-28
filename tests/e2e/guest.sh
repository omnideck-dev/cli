#!/usr/bin/env bash

set -Eeuo pipefail

work_dir="${1:?guest work directory is required}"
expected_version="${2:?expected version is required}"
fixture_image="${3:?fixture image is required}"
test_tier="${4:?test tier is required}"
case "$test_tier" in product|onboarding) ;; *) printf 'Unknown test tier: %s\n' "$test_tier" >&2; exit 2 ;; esac
result_dir="${work_dir}/results"
archive="${work_dir}/omnideck-linux-amd64.tar.gz"
checksum_file="${work_dir}/SHA256SUMS"
binary="${work_dir}/bin/omnideck"
config_dir="${work_dir}/config"
registries_conf="${work_dir}/registries.conf"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
current_step="initialization"
test_status="failed"
pkexec_path=""
pkexec_backup=""

mkdir -p "${result_dir}" "${work_dir}/extracted" "${work_dir}/bin" "${config_dir}"
exec > >(tee -a "${result_dir}/guest.log") 2>&1

inventory() {
  local suffix="$1"
  {
    printf 'timestamp=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    . /etc/os-release
    printf 'os=%s\n' "${PRETTY_NAME}"
    printf 'kernel=%s\n' "$(uname -r)"
    printf 'architecture=%s\n' "$(uname -m)"
    if command -v podman >/dev/null 2>&1; then
      podman --version
      podman ps --all --format 'container={{.Names}}|{{.Status}}' | sort
      podman volume ls --format 'volume={{.Name}}' | sort
      podman images --format 'image={{.Repository}}:{{.Tag}}|{{.ID}}' | sort
    else
      printf 'podman=absent\n'
    fi
  } > "${result_dir}/inventory-${suffix}.txt"
}

cleanup_resources() {
  if command -v podman >/dev/null 2>&1; then
    podman rm -f omnideck >/dev/null 2>&1 || true
    podman volume rm omnideck-home omnideck-state >/dev/null 2>&1 || true
  fi
  rm -rf -- "${config_dir}"
}

restore_pkexec() {
  if [[ -n "${pkexec_backup}" && -e "${pkexec_backup}" ]]; then
    sudo mv -- "${pkexec_backup}" "${pkexec_path}"
  fi
}

write_evidence() {
  local exit_code=$?
  set +e
  restore_pkexec
  if [[ -d "${config_dir}" ]]; then
    tar -czf "${result_dir}/config-on-exit.tar.gz" -C "${config_dir}" .
  fi
  if [[ "${test_status}" != "passed" && "${OMNIDECK_E2E_KEEP_GUEST_STATE:-0}" != "1" ]]; then
    cleanup_resources
  fi
  inventory after
  printf '{\n  "status": "%s",\n  "lastStep": "%s",\n  "expectedVersion": "%s",\n  "fixtureImage": "%s",\n  "startedAt": "%s",\n  "finishedAt": "%s"\n}\n' \
    "${test_status}" "${current_step}" "${expected_version}" "${fixture_image}" \
    "${started_at}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${result_dir}/summary.json"
  if [[ "${test_status}" == "passed" ]]; then
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
      '<testsuite name="omnideck-vm-e2e" tests="3" failures="0"><testcase classname="vm-e2e" name="portable-cli-contract"/><testcase classname="vm-e2e" name="clean-install-and-tui"/><testcase classname="vm-e2e" name="unattended-lifecycle"/></testsuite>' \
      > "${result_dir}/junit.xml"
  else
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
      "<testsuite name=\"omnideck-vm-e2e\" tests=\"1\" failures=\"1\"><testcase classname=\"vm-e2e\" name=\"${current_step}\"><failure message=\"See guest.log and terminal transcripts\"/></testcase></testsuite>" \
      > "${result_dir}/junit.xml"
  fi
  tar -czf "${work_dir}/evidence.tar.gz" -C "${result_dir}" .
  exit "${exit_code}"
}
trap write_evidence EXIT

current_step="clean-host precondition"
inventory before
if [[ "$test_tier" == onboarding ]] && command -v podman >/dev/null 2>&1; then
  printf 'The install scenario requires a clean mutable guest with Podman absent.\n' >&2
  exit 1
fi
if [[ "$test_tier" == product ]]; then
  command -v podman >/dev/null 2>&1 || { printf 'The product scenario requires Podman in the certified baseline.\n' >&2; exit 1; }
  podman info >/dev/null
fi
[[ ! -e "${config_dir}/instances/omnideck.yaml" ]] || {
  printf 'The isolated test configuration unexpectedly contains an existing instance.\n' >&2
  exit 1
}

current_step="install release archive"
(cd "${work_dir}" && sha256sum --check --strict "$(basename "${checksum_file}")")
tar -xzf "${archive}" -C "${work_dir}/extracted"
install -m 0755 "${work_dir}/extracted/omnideck" "${binary}"
"${binary}" --version | tee "${result_dir}/version.txt"
grep -Fq "omnideck version ${expected_version}" "${result_dir}/version.txt"
"${binary}" install --help > "${result_dir}/install-help.txt"
grep -Fq 'Walks through setting up one Omnideck instance.' "${result_dir}/install-help.txt"
grep -Fq 'add, install, setup' "${result_dir}/install-help.txt"

current_step="portable CLI contract"
tar -xzf "${work_dir}/contracts.tar.gz" -C "${work_dir}"
"${work_dir}/releasecontract" \
  --binary "${binary}" \
  --mode portable \
  --expected-version "${expected_version}" \
  --expected-os linux \
  --expected-arch amd64 \
  --contracts "${work_dir}/contracts" \
  --report "${result_dir}/portable-contract.json" \
  --junit "${result_dir}/portable-contract.xml"

current_step="configure fixture registry"
cat > "${registries_conf}" <<EOF
[[registry]]
location = "${fixture_image%%/*}"
insecure = true
EOF

current_step="${test_tier} install journey"
# SSH does not have a graphical PolicyKit agent. Hide pkexec only for this
# disposable terminal journey so the CLI takes its documented sudo fallback;
# the trap restores the exact file before the guest is inventoried or reset.
if [[ "$test_tier" == onboarding ]]; then
  pkexec_path="$(command -v pkexec || true)"
  if [[ -n "${pkexec_path}" ]]; then
    pkexec_backup="${pkexec_path}.omnideck-e2e-disabled"
    [[ ! -e "${pkexec_backup}" ]]
    sudo mv -- "${pkexec_path}" "${pkexec_backup}"
    printf 'Temporarily hid %s to exercise terminal sudo fallback.\n' "${pkexec_path}" \
      > "${result_dir}/terminal-elevation.txt"
  fi
  env PATH="${work_dir}/elevation-bin:${PATH}" python3 "${work_dir}/terminal_driver.py" install \
    --binary "${binary}" \
    --config-dir "${config_dir}" \
    --registries-conf "${registries_conf}" \
    --fixture-image "${fixture_image}" \
    --artifact-dir "${result_dir}"
  restore_pkexec
else
  env OMNIDECK_CONFIG_DIR="${config_dir}" CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
    "${binary}" --no-color install --plain --name omnideck --image "${fixture_image}" \
    | tee "${result_dir}/install-plain.txt"
fi

current_step="installed behavior"
env OMNIDECK_CONFIG_DIR="${config_dir}" CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
  "${binary}" --no-color --name omnideck status | tee "${result_dir}/status.txt"
grep -Fq 'running' "${result_dir}/status.txt"
curl --fail --silent --show-error --max-time 10 http://127.0.0.1:2337 > "${result_dir}/web-ui.html"
grep -Fq 'omnideck hardware fixture ready' "${result_dir}/web-ui.html"
podman container inspect omnideck > "${result_dir}/container-inspect.json"
podman volume inspect omnideck-home omnideck-state > "${result_dir}/volume-inspect.json"
grep -Eq '^runtime:[[:space:]]+podman$' "${config_dir}/settings.yaml"
grep -Eq '^container_name:[[:space:]]+omnideck$' "${config_dir}/instances/omnideck.yaml"
grep -Fq "image: ${fixture_image}" "${config_dir}/instances/omnideck.yaml"

current_step="unattended command and update contract"
env OMNIDECK_CONFIG_DIR="${config_dir}" CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
  "${binary}" --json list > "${result_dir}/list.json"
env OMNIDECK_CONFIG_DIR="${config_dir}" CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
  "${binary}" --json --name omnideck status > "${result_dir}/status.json"
python3 - "${result_dir}/list.json" "${result_dir}/status.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    instances = json.load(stream)
assert len(instances) == 1 and instances[0]["name"] == "omnideck", instances
with open(sys.argv[2], encoding="utf-8") as stream:
    status = json.load(stream)
assert status["container"] == "omnideck" and status["status"] == "running", status
PY
podman exec omnideck sh -c 'printf "%s\n" update-volume-marker > /home/omnideck/update-volume-marker'
env OMNIDECK_CONFIG_DIR="${config_dir}" CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
  "${binary}" --no-color --name omnideck config set memory 512m | tee "${result_dir}/config-set.txt"
grep -Fq 'Set memory = 512m' "${result_dir}/config-set.txt"
env OMNIDECK_CONFIG_DIR="${config_dir}" CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
  "${binary}" --no-color --name omnideck update --plain | tee "${result_dir}/update-plain.txt"
grep -Fq 'Omnideck is up to date: http://localhost:2337' "${result_dir}/update-plain.txt"
[[ "$(podman exec omnideck cat /home/omnideck/update-volume-marker)" == "update-volume-marker" ]]
curl --fail --silent --show-error --max-time 10 http://127.0.0.1:2337 > /dev/null

current_step="tui management journey"
python3 "${work_dir}/terminal_driver.py" manage \
  --binary "${binary}" \
  --config-dir "${config_dir}" \
  --registries-conf "${registries_conf}" \
  --fixture-image "${fixture_image}" \
  --artifact-dir "${result_dir}"

current_step="removal cleanup contract"
! podman container inspect omnideck >/dev/null 2>&1
! podman volume inspect omnideck-home >/dev/null 2>&1
! podman volume inspect omnideck-state >/dev/null 2>&1
[[ ! -e "${config_dir}/instances/omnideck.yaml" ]]

current_step="unattended CLI lifecycle"
chmod +x "${work_dir}/hardware-run.sh"
OMNIDECK_HARDWARE_CLI="${binary}" \
OMNIDECK_HARDWARE_ENGINE=podman \
OMNIDECK_HARDWARE_TEST_IMAGE="${fixture_image}" \
OMNIDECK_HARDWARE_OUTPUT_DIR="${result_dir}/unattended" \
CONTAINERS_REGISTRIES_CONF="${registries_conf}" \
  "${work_dir}/hardware-run.sh"
python3 - "${result_dir}/unattended/summary.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    result = json.load(stream)
assert result["status"] == "passed", result
PY

current_step="complete"
test_status="passed"
printf 'PASS: portable, attended terminal, and unattended CLI journeys completed.\n'
