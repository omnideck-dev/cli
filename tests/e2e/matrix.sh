#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
source "${script_dir}/_common.sh"
lanes_csv="${OMNIDECK_VM_E2E_LANES:-appimage,deb,rpm,windows}"
assume_yes=0
suite="${OMNIDECK_VM_E2E_SUITE:-product}"

while (($#)); do
  case "$1" in
    --lanes) lanes_csv="${2:?--lanes requires a value}"; shift 2 ;;
    --suite) suite="${2:?--suite requires a value}"; shift 2 ;;
    --yes) assume_yes=1; shift ;;
    -h|--help) printf 'Usage: %s [--suite product|onboarding|all] [--lanes appimage,deb,rpm,windows] [--yes]\n' "$0"; exit 0 ;;
    *) printf 'Unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done

case "$suite" in product|onboarding|all) ;; *) printf 'Unsupported suite: %s\n' "$suite" >&2; exit 2 ;; esac

require_lab
IFS=',' read -r -a lanes <<<"$lanes_csv"
for lane in "${lanes[@]}"; do
  case "$lane" in appimage|deb|rpm|windows) ;; *) printf 'Unsupported lane: %s\n' "$lane" >&2; exit 2 ;; esac
done
if [[ "$suite" == all ]]; then tiers=(product onboarding); else tiers=("$suite"); fi
for tier in "${tiers[@]}"; do
  if [[ "$tier" == product ]]; then profile=product-ready; else profile=onboarding-clean; fi
  "${lab_dir}/lab.sh" preflight cli "$profile" --lanes "$lanes_csv" >/dev/null
done

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
run_root="$("${lab_dir}/lab.sh" artifact-path cli matrix "$run_id")"
status_file="${run_root}/lane-status.tsv"
mkdir -p "${run_root}/lanes"
: > "$status_file"
source_state
"${lab_dir}/lab.sh" evidence-init "$run_root" cli matrix "$run_id" "$source_short" multi clean \
  "phase=prepared" "testTier=${suite}" "lanes=${lanes_csv}" "sourceDirty=${source_dirty}" "sourceFingerprint=${source_fingerprint}"
finalized=0
matrix_signal=0
active_lane_pid=""
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
interrupt_matrix() {
  local status="$1" signal_name="$2"
  matrix_signal="$status"
  if [[ "$signal_name" == INT ]]; then signal_name=TERM; fi
  if [[ "$active_lane_pid" =~ ^[0-9]+$ ]] && kill -0 "$active_lane_pid" 2>/dev/null; then
    kill -s "$signal_name" "$active_lane_pid" 2>/dev/null || true
  fi
}
trap 'interrupt_matrix 130 INT' INT
trap 'interrupt_matrix 143 TERM' TERM

status=0
for tier in "${tiers[@]}"; do
 for lane in "${lanes[@]}"; do
  arguments=(--vm "$lane" --suite "$tier")
  [[ "$assume_yes" == 0 ]] || arguments+=(--yes)
  lane_status=0
  lane_key="${tier}-${lane}"
  lane_dir="${run_root}/lanes/${lane_key}"
  mkdir -p "$lane_dir"
  OMNIDECK_VM_E2E_OUTPUT_DIR="$lane_dir" \
    "$script_dir/run.sh" "${arguments[@]}" > >(tee "${lane_dir}/host.log") 2>&1 &
  active_lane_pid=$!
  wait "$active_lane_pid" || lane_status=$?
  if [[ "$matrix_signal" != 0 ]]; then
    wait "$active_lane_pid" 2>/dev/null || true
    active_lane_pid=""
    printf '%s\tcanceled\tlanes/%s\n' "$lane_key" "$lane_key" >> "$status_file"
    exit "$matrix_signal"
  fi
  active_lane_pid=""
  if [[ "$lane_status" == 0 ]]; then
    printf '%s\tpassed\tlanes/%s\n' "$lane_key" "$lane_key" >> "$status_file"
  else
    printf '%s\tfailed\tlanes/%s\n' "$lane_key" "$lane_key" >> "$status_file"
    status=1
  fi
 done
done
if [[ "$status" == 0 ]]; then
  "${lab_dir}/lab.sh" evidence-finish "$run_root" passed
else
  "${lab_dir}/lab.sh" evidence-finish "$run_root" failed || true
fi
finalized=1
printf 'CLI matrix evidence: %s\n' "$run_root"
exit "$status"
