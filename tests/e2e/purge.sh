#!/usr/bin/env bash

set -Eeuo pipefail

lab_dir="${OMNIDECK_VM_LAB_DIR:-}"
assume_yes=0

usage() {
  cat <<'EOF'
Usage: ./tests/e2e/purge.sh [--yes] RUN_DIRECTORY

Delete one VM E2E run folder and any retained discarded disk or TPM state
listed by that run. RUN_DIRECTORY must be a direct child of the external
lab's artifacts/cli-e2e directory and contain run.json.
EOF
}

if [[ "${1:-}" == "--yes" ]]; then
  assume_yes=1
  shift
fi
run_dir_input="${1:-}"
[[ -n "${run_dir_input}" && $# == 1 ]] || { usage >&2; exit 2; }
[[ -n "${lab_dir}" ]] || { printf 'Set OMNIDECK_VM_LAB_DIR to the external VM lab root.\n' >&2; exit 2; }
lab_dir="$(cd "${lab_dir}" && pwd -P)"

artifact_root="$(realpath -e "${lab_dir}/artifacts/cli-e2e")"
run_dir="$(realpath -e "${run_dir_input}")"
[[ "$(dirname "${run_dir}")" == "${artifact_root}" ]] || {
  printf 'Refusing to purge a path outside %s: %s\n' "${artifact_root}" "${run_dir}" >&2
  exit 1
}
[[ -f "${run_dir}/run.json" ]] || { printf 'Missing E2E run marker: %s/run.json\n' "${run_dir}" >&2; exit 1; }

run_name="$(basename "${run_dir}")"
manifest="${run_dir}/discarded-created.txt"
printf 'Run artifacts: '
du -sh -- "${run_dir}"
if [[ -s "${manifest}" ]]; then
  printf 'Retained disposable overlays:\n'
  while IFS= read -r overlay; do
    [[ -n "${overlay}" ]] || continue
    if [[ -e "${overlay}" ]]; then
      du -sh -- "${overlay}"
    fi
  done < "${manifest}"
fi

if [[ "${assume_yes}" != "1" ]]; then
  [[ -t 0 ]] || { printf 'Re-run interactively or pass --yes.\n' >&2; exit 2; }
  printf 'Type %s to permanently purge this run: ' "${run_name}"
  read -r confirmation
  [[ "${confirmation}" == "${run_name}" ]] || { printf 'Canceled.\n'; exit 1; }
fi

if [[ -f "${manifest}" ]]; then
  while IFS= read -r overlay; do
    [[ -n "${overlay}" && -e "${overlay}" ]] || continue
    overlay_parent="$(dirname "${overlay}")"
    overlay_name="$(basename "${overlay}")"
    [[ "${overlay_parent}" == "${lab_dir}/discarded" ]] || {
      printf 'Refusing unexpected overlay parent: %s\n' "${overlay}" >&2
      exit 1
    }
    case "${overlay_name}" in
      appimage.qcow2.*|deb.qcow2.*|rpm.qcow2.*|windows.qcow2.*|windows-tpm.*) ;;
      *)
        printf 'Refusing unexpected overlay name: %s\n' "${overlay}" >&2
        exit 1
        ;;
    esac
    if [[ -d "${overlay}" ]]; then
      rm -r -- "${overlay}"
    else
      unlink "${overlay}"
    fi
  done < "${manifest}"
fi

rm -r -- "${run_dir}"
printf 'Purged VM E2E run: %s\n' "${run_name}"
