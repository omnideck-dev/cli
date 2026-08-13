#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
vm="${OMNIDECK_VM_LAB_VM:-appimage}"
engine="${OMNIDECK_HARDWARE_ENGINE:-podman}"

if [[ "${engine}" != "podman" ]]; then
  printf 'The VM E2E suite uses the release-gating Podman runtime; got OMNIDECK_HARDWARE_ENGINE=%q.\n' "${engine}" >&2
  exit 2
fi

case "${vm}" in
  appimage|ubuntu|deb|debian|rpm|fedora|windows) ;;
  atomic|silverblue)
    printf 'The immutable Silverblue guest does not satisfy the clean-install E2E precondition; use the Desktop atomic-host lane for packaged compatibility.\n' >&2
    exit 2
    ;;
  *)
    printf 'Unsupported VM %q. Use appimage, deb, rpm, windows, or a canonical mutable-guest alias.\n' "${vm}" >&2
    exit 2
    ;;
esac

printf 'run-local-hardware.sh now delegates to the canonical VM E2E lane.\n'
printf 'The lane prepares a content-addressed build, acquires the lease, records evidence, and restores the clean baseline.\n'
exec "${repo_root}/tests/e2e/run.sh" --vm "${vm}" "$@"
