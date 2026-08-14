#!/usr/bin/env bash

set -Eeuo pipefail

export LANG=C
export LC_ALL=C

work_dir="${1:?guest work directory is required}"
expected_version="${2:?expected version is required}"
fixture_image="${3:?fixture image is required}"
instance=''
test_instance_selected=0
web_port=2337
additional_name=omnideck2
additional_port=2338
registry_port=46864
registry_container="omnideck-hw-macos-e2e-registry"
ownership_marker="$HOME/.omnideck-lab/state/cli-e2e-instance"
published_image="localhost:${registry_port}/${fixture_image#localhost/}"
result_dir="$work_dir/results"
archive="$work_dir/omnideck-darwin-arm64.tar.gz"
checksum_file="$work_dir/SHA256SUMS"
binary="$work_dir/bin/omnideck"
config_dir="$work_dir/config"
registries_conf="$work_dir/registries.conf"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
current_step=initialization
test_status=failed

mkdir -p "$result_dir" "$work_dir/extracted" "$work_dir/bin" "$config_dir"
exec > >(tee -a "$result_dir/guest.log") 2>&1

inventory() {
  local suffix="$1"
  {
    printf 'timestamp=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'os=%s\narchitecture=%s\n' "$(sw_vers -productVersion)" "$(uname -m)"
    podman --version
    podman machine list --format json
    podman ps --all --format 'container={{.Names}}|{{.Status}}' | sort
    podman volume ls --format 'volume={{.Name}}' | sort
  } > "$result_dir/inventory-$suffix.txt"
}

cleanup_resources() {
  if [[ "$test_instance_selected" == 1 && "$instance" =~ ^omnideck[0-9]*$ ]]; then
    podman rm -f "$instance" >/dev/null 2>&1 || true
    podman volume rm -f "$instance-home" "$instance-state" >/dev/null 2>&1 || true
  fi
  rm -f -- "$ownership_marker"
  podman rm -f "$registry_container" >/dev/null 2>&1 || true
  podman rmi -f "$published_image" "$fixture_image" >/dev/null 2>&1 || true
  rm -rf -- "$config_dir"
}

write_evidence() {
  local exit_code=$?
  set +e
  [[ ! -d "$config_dir" ]] || tar -czf "$result_dir/config-on-exit.tar.gz" -C "$config_dir" .
  cleanup_resources
  inventory after
  printf '{\n  "status": "%s",\n  "lastStep": "%s",\n  "expectedVersion": "%s",\n  "fixtureImage": "%s",\n  "platform": "darwin",\n  "architecture": "arm64",\n  "podmanSetup": "excluded-ready-runtime",\n  "startedAt": "%s",\n  "finishedAt": "%s"\n}\n' \
    "$test_status" "$current_step" "$expected_version" "$fixture_image" \
    "$started_at" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$result_dir/summary.json"
  if [[ "$test_status" == passed ]]; then
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
      '<testsuite name="omnideck-macos-e2e" tests="3" failures="0"><testcase classname="macos-e2e" name="portable-cli-contract"/><testcase classname="macos-e2e" name="ready-runtime-install-and-tui"/><testcase classname="macos-e2e" name="unattended-lifecycle"/></testsuite>' \
      > "$result_dir/junit.xml"
  else
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
      "<testsuite name=\"omnideck-macos-e2e\" tests=\"1\" failures=\"1\"><testcase classname=\"macos-e2e\" name=\"$current_step\"><failure message=\"See guest.log and terminal transcripts\"/></testcase></testsuite>" \
      > "$result_dir/junit.xml"
  fi
  exit "$exit_code"
}
trap write_evidence EXIT
trap 'printf "ERROR step=%s line=%s command=%q\\n" "$current_step" "$LINENO" "$BASH_COMMAND" >&2' ERR

current_step='ready-runtime precondition'
[[ "$(uname -s)/$(uname -m)" == Darwin/arm64 ]]
podman info >/dev/null
inventory before
for suffix in '' $(seq 2 9999); do
  candidate="omnideck${suffix}"
  if ! podman container inspect "$candidate" >/dev/null 2>&1; then instance="$candidate"; break; fi
done
[[ -n "$instance" ]]
test_instance_selected=1
mkdir -p "$(dirname "$ownership_marker")"
printf '%s\n' "$instance" > "$ownership_marker"
for suffix in '' $(seq 2 9999); do
  candidate="omnideck${suffix}"
  [[ "$candidate" == "$instance" ]] && continue
  if ! podman container inspect "$candidate" >/dev/null 2>&1; then additional_name="$candidate"; break; fi
done
web_port="$(python3 - <<'PY'
import socket
for port in range(2337, 65536):
    sock=socket.socket()
    try: sock.bind(('127.0.0.1', port))
    except OSError: continue
    finally: sock.close()
    print(port); break
PY
)"
additional_port="$(python3 - "$web_port" <<'PY'
import socket, sys
for port in range(int(sys.argv[1]) + 1, 65536):
    sock=socket.socket()
    try: sock.bind(('127.0.0.1', port))
    except OSError: continue
    finally: sock.close()
    print(port); break
PY
)"
[[ "$instance" =~ ^omnideck[0-9]*$ && "$additional_name" =~ ^omnideck[0-9]*$ ]]
[[ "$web_port" =~ ^[0-9]+$ && "$additional_port" =~ ^[0-9]+$ ]]
printf 'selectedInstance=%s\nselectedPort=%s\nadditionalName=%s\nadditionalPort=%s\n' \
  "$instance" "$web_port" "$additional_name" "$additional_port" > "$result_dir/isolation.txt"
[[ ! -e "$config_dir/instances/$instance.yaml" ]]

current_step='install release archive'
(cd "$work_dir" && shasum -a 256 --check "$(basename "$checksum_file")")
tar -xzf "$archive" -C "$work_dir/extracted"
install -m 0755 "$work_dir/extracted/omnideck" "$binary"
"$binary" --version | tee "$result_dir/version.txt"
grep -Fq "omnideck version $expected_version" "$result_dir/version.txt"
"$binary" install --help > "$result_dir/install-help.txt"
grep -Fq 'Walks through setting up one Omnideck instance.' "$result_dir/install-help.txt"
grep -Fq 'add, install, setup' "$result_dir/install-help.txt"

current_step='portable CLI contract'
tar -xzf "$work_dir/contracts.tar.gz" -C "$work_dir"
"$work_dir/releasecontract" \
  --binary "$binary" --mode portable --expected-version "$expected_version" \
  --expected-os darwin --expected-arch arm64 --contracts "$work_dir/contracts" \
  --report "$result_dir/portable-contract.json" --junit "$result_dir/portable-contract.xml"

current_step='load ARM64 fixture image'
podman load --input "$work_dir/fixture.tar" | tee "$result_dir/fixture-load.txt"
printf '[[registry]]\nlocation = "localhost:%s"\ninsecure = true\n' "$registry_port" > "$registries_conf"
podman run -d --name "$registry_container" -p "127.0.0.1:${registry_port}:5000" \
  docker.io/library/registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373
for _ in $(seq 1 30); do
  curl --fail --silent --max-time 2 "http://127.0.0.1:${registry_port}/v2/" >/dev/null && break
  sleep 1
done
curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${registry_port}/v2/" >/dev/null
podman tag "$fixture_image" "$published_image"
podman push --tls-verify=false "$published_image"

current_step='guided install journey with Podman setup excluded'
python3 "$work_dir/terminal_driver.py" macos-install \
  --binary "$binary" --config-dir "$config_dir" --registries-conf "$registries_conf" \
  --fixture-image "$published_image" --artifact-dir "$result_dir" \
  --instance-name "$instance" --web-port "$web_port" \
  --additional-name "$additional_name" --additional-port "$additional_port"

current_step='installed behavior'
env OMNIDECK_CONFIG_DIR="$config_dir" CONTAINERS_REGISTRIES_CONF="$registries_conf" \
  "$binary" --no-color --name "$instance" status | tee "$result_dir/status.txt"
grep -Fq running "$result_dir/status.txt"
curl --fail --silent --show-error --max-time 10 "http://127.0.0.1:$web_port" > "$result_dir/web-ui.html"
grep -Fq 'omnideck hardware fixture ready' "$result_dir/web-ui.html"
podman container inspect "$instance" > "$result_dir/container-inspect.json"
podman volume inspect "$instance-home" "$instance-state" > "$result_dir/volume-inspect.json"
grep -Eq '^runtime:[[:space:]]+podman$' "$config_dir/settings.yaml"
grep -Eq "^container_name:[[:space:]]+$instance$" "$config_dir/instances/$instance.yaml"
grep -Fq "image: $published_image" "$config_dir/instances/$instance.yaml"

current_step='unattended command and update contract'
env OMNIDECK_CONFIG_DIR="$config_dir" CONTAINERS_REGISTRIES_CONF="$registries_conf" \
  "$binary" --json list > "$result_dir/list.json"
env OMNIDECK_CONFIG_DIR="$config_dir" CONTAINERS_REGISTRIES_CONF="$registries_conf" \
  "$binary" --json --name "$instance" status > "$result_dir/status.json"
python3 - "$result_dir/list.json" "$result_dir/status.json" "$instance" <<'PY'
import json, sys
instances=json.load(open(sys.argv[1], encoding='utf-8'))
status=json.load(open(sys.argv[2], encoding='utf-8'))
assert len(instances) == 1 and instances[0]['name'] == sys.argv[3], instances
assert status['container'] == sys.argv[3] and status['status'] == 'running', status
PY
podman exec "$instance" sh -c 'printf "%s\n" update-volume-marker > /home/omnideck/update-volume-marker'
env OMNIDECK_CONFIG_DIR="$config_dir" CONTAINERS_REGISTRIES_CONF="$registries_conf" \
  "$binary" --no-color --name "$instance" config set memory 512m | tee "$result_dir/config-set.txt"
grep -Fq 'Set memory = 512m' "$result_dir/config-set.txt"
env OMNIDECK_CONFIG_DIR="$config_dir" CONTAINERS_REGISTRIES_CONF="$registries_conf" \
  "$binary" --no-color --name "$instance" update --plain | tee "$result_dir/update-plain.txt"
grep -Fq "Omnideck is up to date: http://localhost:$web_port" "$result_dir/update-plain.txt"
[[ "$(podman exec "$instance" cat /home/omnideck/update-volume-marker)" == update-volume-marker ]]

current_step='TUI management journey'
python3 "$work_dir/terminal_driver.py" manage \
  --binary "$binary" --config-dir "$config_dir" --registries-conf "$registries_conf" \
  --fixture-image "$published_image" --artifact-dir "$result_dir" \
  --instance-name "$instance" --web-port "$web_port" \
  --additional-name "$additional_name" --additional-port "$additional_port"

current_step='removal cleanup contract'
! podman container inspect "$instance" >/dev/null 2>&1
! podman volume inspect "$instance-home" >/dev/null 2>&1
! podman volume inspect "$instance-state" >/dev/null 2>&1
[[ ! -e "$config_dir/instances/$instance.yaml" ]]

current_step='unattended CLI lifecycle'
podman rm -f "$registry_container" >/dev/null
chmod +x "$work_dir/hardware-run.sh"
OMNIDECK_HARDWARE_CLI="$binary" \
OMNIDECK_HARDWARE_ENGINE=podman \
OMNIDECK_HARDWARE_REGISTRY_PORT=46864 \
OMNIDECK_HARDWARE_TEST_IMAGE_ARCHIVE="$work_dir/fixture.tar" \
OMNIDECK_HARDWARE_ARCHIVE_IMAGE="$fixture_image" \
OMNIDECK_HARDWARE_OUTPUT_DIR="$result_dir/unattended" \
  "$work_dir/hardware-run.sh"
python3 - "$result_dir/unattended/summary.json" <<'PY'
import json, sys
result=json.load(open(sys.argv[1], encoding='utf-8'))
assert result['status'] == 'passed', result
PY

current_step=complete
test_status=passed
printf 'PASS: macOS portable, ready-runtime attended TUI, management, and unattended CLI journeys completed.\n'
