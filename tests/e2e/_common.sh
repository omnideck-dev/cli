#!/usr/bin/env bash

require_lab() {
  lab_dir="${OMNIDECK_VM_LAB_DIR:-}"
  [[ -n "$lab_dir" ]] || { printf 'Set OMNIDECK_VM_LAB_DIR to the external VM lab root.\n' >&2; exit 2; }
  [[ -x "$lab_dir/lab.sh" ]] || { printf 'Missing executable lab.sh under %s\n' "$lab_dir" >&2; exit 2; }
  lab_dir="$(cd "$lab_dir" && pwd -P)"
  python3 -c 'import json,subprocess,sys; data=json.loads(subprocess.check_output([sys.argv[1], "capabilities", "--json"])); required={"artifact-path","cache-path","lease-cleanup","preflight"}; missing=required-set(data["features"]); assert not missing, f"VM lab lacks: {sorted(missing)}"' "$lab_dir/lab.sh" || {
    printf 'CLI VM E2E requires the OmniDeck VM lab 2.1 capability contract.\n' >&2
    exit 2
  }
}

source_state() {
  source_commit="$(git -C "$repo_root" rev-parse HEAD)"
  source_short="${source_commit:0:12}"
  source_dirty=false
  [[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=normal)" ]] || source_dirty=true
  source_fingerprint="$({
    git -C "$repo_root" diff --binary HEAD --
    while IFS= read -r file; do (cd "$repo_root" && sha256sum "$file"); done < <(git -C "$repo_root" ls-files --others --exclude-standard | sort)
  } | sha256sum | awk '{print substr($1,1,20)}')"
}

builder_key() {
  sha256sum "$repo_root/.devcontainer/Dockerfile" "$repo_root/go.mod" "$repo_root/go.sum" | awk '{print $1}' |
    sha256sum | awk '{print substr($1,1,20)}'
}

prepare_cli_binaries() {
  local target="$1" key cache_dir temporary epoch
  source_state
  ensure_builder
  key="${source_short}-${source_fingerprint}-$(builder_key)-$(printf '%s' "$builder_image_id" | sha256sum | awk '{print substr($1,1,12)}')-${target}"
  cache_dir="$("$lab_dir/lab.sh" cache-path cli "$key")"
  mkdir -p "$(dirname "$cache_dir")"
  if [[ ! -f "$cache_dir/.complete" ]]; then
    temporary="$(mktemp -d "$lab_dir/cache/cli/.${key}.XXXXXX")"
    build_dir="$temporary"
    printf '%s\n' "$builder_image_id" > "$build_dir/builder-image.txt"
    printf '%s\n' "$(builder_key)" > "$build_dir/builder-key.txt"
    epoch="$(git -C "$repo_root" show -s --format=%ct HEAD)"
    printf 'Building release-shaped %s CLI artifacts before leasing a VM.\n' "$target"
    if [[ "$target" == windows ]]; then
      docker run --rm --entrypoint /bin/bash \
        --user "$(id -u):$(id -g)" \
        --env GOCACHE=/tmp/omnideck-go-build --env GOPATH=/tmp/omnideck-go \
        -v "$repo_root:/workspace:ro" -v "$temporary:/out" -w /workspace "$builder_image" \
        -c "cp -a /workspace /tmp/omnideck-source && cd /tmp/omnideck-source && go tool go-winres make --arch amd64 --out rsrc --file-version 0.0.0.0 --product-version 0.0.0.0 && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags '-X main.version=vm-e2e-${source_short} -X main.commit=${source_short} -X main.date=vm-e2e' -o /out/omnideck.exe . && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/releasecontract.exe ./tests/releasecontract"
      (cd "$temporary" && TZ=UTC touch -d "@${epoch}" omnideck.exe releasecontract.exe && zip -X -q omnideck-windows-amd64.zip omnideck.exe)
      (cd "$temporary" && sha256sum omnideck-windows-amd64.zip > SHA256SUMS)
      sha256sum "$temporary/omnideck.exe" > "$temporary/omnideck.exe.sha256"
    else
      docker run --rm --entrypoint /bin/bash \
        --user "$(id -u):$(id -g)" \
        --env GOCACHE=/tmp/omnideck-go-build --env GOPATH=/tmp/omnideck-go \
        -v "$repo_root:/workspace:ro" -v "$temporary:/out" -w /workspace "$builder_image" \
        -c "go build -trimpath -buildvcs=false -ldflags '-X main.version=vm-e2e-${source_short} -X main.commit=${source_short} -X main.date=vm-e2e' -o /out/omnideck . && go build -trimpath -buildvcs=false -o /out/releasecontract ./tests/releasecontract"
      chmod +x "$temporary/omnideck" "$temporary/releasecontract"
      tar --sort=name --mtime="@${epoch}" --owner=0 --group=0 --numeric-owner -cf - -C "$temporary" omnideck | gzip -n > "$temporary/omnideck-linux-amd64.tar.gz"
      (cd "$temporary" && sha256sum omnideck-linux-amd64.tar.gz > SHA256SUMS)
      sha256sum "$temporary/omnideck" > "$temporary/omnideck.sha256"
    fi
    tar --sort=name --mtime="@${epoch}" --owner=0 --group=0 --numeric-owner -cf - -C "$repo_root" contracts | gzip -n > "$temporary/contracts.tar.gz"
    : > "$temporary/.complete"
    if ! mv -T -- "$temporary" "$cache_dir" 2>/dev/null; then
      find "$temporary" -type f -delete
      find "$temporary" -depth -type d -empty -delete
    fi
  fi
  cli_build_cache="$cache_dir"
  cli_build_key="$key"
  touch "$cache_dir"
}

ensure_builder() {
  local key
  key="$(builder_key)"
  builder_image="${OMNIDECK_CLI_BUILDER_IMAGE:-omnideck-cli-builder:${key}}"
  if ! docker image inspect "$builder_image" >/dev/null 2>&1; then
    printf 'Building content-addressed Go builder: %s\n' "$builder_image"
    docker build --tag "$builder_image" --file "$repo_root/.devcontainer/Dockerfile" "$repo_root/.devcontainer"
  fi
  builder_image_id="$(docker image inspect "$builder_image" --format '{{.Id}}')"
}

write_source_metadata() {
  python3 - "$output_dir/source.json" "$source_commit" "$source_dirty" "$builder_image" <<'PY'
import json, sys
path, commit, dirty, builder = sys.argv[1:]
with open(path, "w") as handle:
    json.dump({"schemaVersion": 1, "sourceCommit": commit, "sourceDirty": dirty == "true", "builderImage": builder}, handle, indent=2, sort_keys=True)
    handle.write("\n")
PY
}
