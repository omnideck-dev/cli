#!/usr/bin/env bash

set -Eeuo pipefail

lab_dir="${1:?lab directory is required}"
evidence_dir="${2:?evidence directory is required}"

mkdir -p "${evidence_dir}"
"${lab_dir}/lab.sh" screenshot windows "${evidence_dir}/windows-uac.png"
"${lab_dir}/lab.sh" send-keys windows alt-y
