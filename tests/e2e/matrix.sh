#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
source "${script_dir}/_common.sh"
lanes_csv="${OMNIDECK_VM_E2E_LANES:-appimage,deb,rpm,windows}"
assume_yes=0

while (($#)); do
  case "$1" in
    --lanes) lanes_csv="${2:?--lanes requires a value}"; shift 2 ;;
    --yes) assume_yes=1; shift ;;
    -h|--help) printf 'Usage: %s [--lanes appimage,deb,rpm,windows] [--yes]\n' "$0"; exit 0 ;;
    *) printf 'Unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done

require_lab
IFS=',' read -r -a lanes <<<"$lanes_csv"
for lane in "${lanes[@]}"; do
  case "$lane" in appimage|deb|rpm|windows) ;; *) printf 'Unsupported lane: %s\n' "$lane" >&2; exit 2 ;; esac
done
"${lab_dir}/lab.sh" preflight cli release-clean --lanes "$lanes_csv" >/dev/null

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
run_root="$("${lab_dir}/lab.sh" artifact-path cli matrix "$run_id")"
status_file="${run_root}/lane-status.tsv"
mkdir -p "${run_root}/lanes"
: > "$status_file"
source_state
"${lab_dir}/lab.sh" evidence-init "$run_root" cli matrix "$run_id" "$source_short" multi clean \
  "phase=prepared" "lanes=${lanes_csv}" "sourceDirty=${source_dirty}" "sourceFingerprint=${source_fingerprint}"
finalized=0
finish_incomplete() {
  local exit_status=$?
  set +e
  if [[ "$finalized" != 1 ]]; then
    "${lab_dir}/lab.sh" evidence-finish "$run_root" failed || true
    printf 'CLI matrix evidence: %s\n' "$run_root"
  fi
  return "$exit_status"
}
trap finish_incomplete EXIT

status=0
for lane in "${lanes[@]}"; do
  arguments=(--vm "$lane")
  [[ "$assume_yes" == 0 ]] || arguments+=(--yes)
  lane_status=0
  lane_dir="${run_root}/lanes/${lane}"
  mkdir -p "$lane_dir"
  OMNIDECK_VM_E2E_OUTPUT_DIR="$lane_dir" \
    "$script_dir/run.sh" "${arguments[@]}" > >(tee "${lane_dir}/host.log") 2>&1 || lane_status=$?
  if [[ "$lane_status" == 0 ]]; then
    printf '%s\tpassed\tlanes/%s\n' "$lane" "$lane" >> "$status_file"
  else
    printf '%s\tfailed\tlanes/%s\n' "$lane" "$lane" >> "$status_file"
    status=1
  fi
done
if [[ "$status" == 0 ]]; then
  "${lab_dir}/lab.sh" evidence-finish "$run_root" passed
else
  "${lab_dir}/lab.sh" evidence-finish "$run_root" failed || true
fi
finalized=1
printf 'CLI matrix evidence: %s\n' "$run_root"
exit "$status"
