#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
lab_dir="${OMNIDECK_VM_LAB_DIR:-}"
vm="${OMNIDECK_VM_LAB_VM:-atomic}"
user="${OMNIDECK_VM_LAB_USER:-tester}"
ssh_host="${OMNIDECK_VM_LAB_HOST:-127.0.0.1}"
builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:local}"
engine="${OMNIDECK_HARDWARE_ENGINE:-podman}"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
safe_run_id="$(printf '%s' "${run_id}" | tr -cd '[:alnum:]._-')"
remote_root="/tmp/omnideck-cli-hardware-${safe_run_id}"
output_dir="${OMNIDECK_VM_LAB_OUTPUT_DIR:-${lab_dir}/artifacts/cli-hardware/${safe_run_id}}"

case "${vm}" in
  appimage) default_ssh_port=2221 ;;
  deb) default_ssh_port=2222 ;;
  rpm) default_ssh_port=2223 ;;
  atomic) default_ssh_port=2224 ;;
  windows)
    printf 'The POSIX hardware helper does not run the Windows procedure; use the manual viewer workflow.\n' >&2
    exit 2
    ;;
  *)
    printf 'Unsupported VM %q. Use appimage, deb, rpm, atomic, or windows.\n' "${vm}" >&2
    exit 2
    ;;
esac

ssh_port="${OMNIDECK_VM_LAB_SSH_PORT:-${default_ssh_port}}"
key_file="${OMNIDECK_VM_LAB_KEY:-${lab_dir}/keys/id_ed25519}"
known_hosts="${OMNIDECK_VM_LAB_KNOWN_HOSTS:-${lab_dir}/runtime/known_hosts}"

[[ -n "${lab_dir}" ]] || { printf 'Set OMNIDECK_VM_LAB_DIR to the external VM lab root.\n' >&2; exit 2; }
[[ -x "${lab_dir}/lab.sh" ]] || { printf 'Missing executable lab.sh under %s\n' "${lab_dir}" >&2; exit 2; }
[[ -f "${key_file}" && -f "${known_hosts}" ]] || { printf 'Missing lab SSH key or known-hosts file.\n' >&2; exit 2; }
command -v docker >/dev/null 2>&1 || { printf 'Docker is required to run the pinned Go builder container.\n' >&2; exit 2; }
docker image inspect "${builder_image}" >/dev/null 2>&1 || {
  printf 'Builder image %q is not available. Build it from .devcontainer/Dockerfile first.\n' "${builder_image}" >&2
  exit 2
}

mkdir -p "${output_dir}"
binary="${output_dir}/omnideck"
source_commit="$(git -C "${repo_root}" rev-parse --short HEAD)"
ldflags="-X main.version=local-vm -X main.commit=${source_commit}"

printf 'Building %s from %s in %s\n' "${binary}" "${source_commit}" "${builder_image}"
docker run --rm --entrypoint /bin/zsh \
  --user "$(id -u):$(id -g)" \
  --env GOCACHE=/tmp/omnideck-go-build \
  --env GOPATH=/tmp/omnideck-go \
  -v "${repo_root}:/workspace" \
  -v "${output_dir}:/out" \
  -w /workspace "${builder_image}" \
  -c "go build -trimpath -buildvcs=false -ldflags \"${ldflags}\" -o /out/omnideck ."
chmod +x "${binary}"
sha256sum "${binary}" | tee "${output_dir}/omnideck.sha256"

ssh_options=(
  -i "${key_file}"
  -o "UserKnownHostsFile=${known_hosts}"
  -o StrictHostKeyChecking=yes
  -o ConnectTimeout=8
)
remote="${user}@${ssh_host}"
remote_staged=0

cleanup_remote() {
  local status=$?
  if [[ "${remote_staged}" == "1" ]]; then
    case "${remote_root}" in
      /tmp/omnideck-cli-hardware-[[:alnum:]_.-]*)
        ssh "${ssh_options[@]}" -p "${ssh_port}" "${remote}" "rm -rf -- '${remote_root}'" >/dev/null 2>&1 || true
        ;;
      *)
        printf 'Refusing to remove unexpected remote path: %s\n' "${remote_root}" >&2
        status=1
        ;;
    esac
  fi
  exit "${status}"
}
trap cleanup_remote EXIT

ssh "${ssh_options[@]}" -p "${ssh_port}" "${remote}" "mkdir -p '${remote_root}'"
remote_staged=1
scp -P "${ssh_port}" "${ssh_options[@]}" "${binary}" "${remote}:${remote_root}/omnideck"
scp -r -P "${ssh_port}" "${ssh_options[@]}" "${repo_root}/tests/hardware" "${remote}:${remote_root}/"

set +e
ssh "${ssh_options[@]}" -p "${ssh_port}" "${remote}" \
  "chmod +x '${remote_root}/omnideck' '${remote_root}/hardware/run.sh' && \
   OMNIDECK_HARDWARE_CLI='${remote_root}/omnideck' \
   OMNIDECK_HARDWARE_ENGINE='${engine}' \
   OMNIDECK_HARDWARE_OUTPUT_DIR='${remote_root}/artifacts' \
   '${remote_root}/hardware/run.sh'"
test_status=$?
set -e

scp -r -P "${ssh_port}" "${ssh_options[@]}" "${remote}:${remote_root}/artifacts/." "${output_dir}/"

printf 'Artifacts: %s\n' "${output_dir}"
exit "${test_status}"
