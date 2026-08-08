# Omnideck CLI testing policy

This document is the authoritative source for Omnideck CLI test requirements,
release evidence, supported test matrices, and prerelease promotion gates.
[RELEASING.md](RELEASING.md) defines the mechanics for tagging, approving, and
publishing a release. Suite-specific operating instructions live beside each
suite.

A workflow, public contract, supported target, or promotion requirement must be
updated here in the same pull request that changes it. A passing check proves
only the behavior described by its test layer; unavailable hardware or an
unexecuted manual procedure is recorded as blocked coverage, never as a pass.

## Test layers

Omnideck uses four separate layers. No layer substitutes for a later layer.

| Layer | Implementation | Required evidence |
|---|---|---|
| Source | Go unit and smoke tests, formatting, module consistency, vet, staticcheck, actionlint, race detection, dependency review, and vulnerability scanning | Command result or GitHub Actions run for the exact commit |
| Release contract | [`tests/releasecontract`](tests/releasecontract/README.md) | JSON and optional JUnit reports for the exact binary or archive |
| Hardware lifecycle | [`tests/hardware`](tests/hardware/README.md) | Harness report and diagnostics from a dedicated machine using Podman |
| Manual journey | [`tests/manual`](tests/manual/README.md) | A completed procedure with host inventory, commands, observations, cleanup, and pass/fail/blocked result |

The source and release-contract layers are non-destructive. Hardware and manual
tests may create containers, volumes, machine state, or operating-system state
and therefore run only on dedicated machines or disposable virtual machines.

## Source verification

`make verify` is the canonical local source gate. It runs:

- `gofmt` checking;
- `go mod tidy -diff`;
- `go vet ./...`;
- staticcheck with the version and checks configured in the `Makefile`;
- actionlint with the version configured in the `Makefile`;
- `go test ./...`; and
- govulncheck with the version configured in the `Makefile`.

Run the race detector separately with `make race`.

The `CI` workflow applies these requirements to pull requests and `main`:

- `quality` runs the formatting, module, vet, staticcheck, and workflow checks;
- `vulnerability-check` verifies downloaded modules and scans reachable code;
- `race` runs the Go test suite with the race detector;
- native test jobs run vet, tests, a build, and the portable release contract
  on Linux x64, Linux ARM64, macOS ARM64, and Windows x64;
- cross-build jobs compile macOS x64 and Windows ARM64; and
- `dependency-review` runs when the event is a pull request.

CodeQL is a required repository check and runs independently of `CI`. A commit
is not release-ready until every required CI and CodeQL check is successful.

## Release contract

The release contract treats the CLI executable as a black box and does not
import implementation packages. It accepts either an extracted binary with
`--binary` or a release archive with `--archive`.

`artifact` mode extracts the archive when necessary and verifies that the
binary is nonempty and has the expected executable format and architecture.

`portable` mode includes the artifact checks and executes bounded,
non-destructive commands with closed stdin and a temporary
`OMNIDECK_CONFIG_DIR`. It verifies:

- checked-in JSON schemas compile;
- text and JSON version output, including the expected embedded version;
- root and subcommand help surfaces;
- bare non-interactive behavior;
- structured JSON success and error output with clean stderr;
- JSON contract 2 and runtime status schema 4;
- early argument and dispatch errors remain machine-readable;
- ambiguous instance selection never prompts in automation; and
- destructive removal requires explicit flags.

Portable mode does not invoke Podman or Docker. The schemas under `contracts/`
are part of the public process boundary and are executed by the contract suite,
not merely documented examples.

Example for an extracted binary:

```sh
go run ./tests/releasecontract \
  --binary /path/to/omnideck \
  --mode portable \
  --expected-version v0.10.0-alpha.2 \
  --expected-os linux \
  --expected-arch amd64 \
  --report artifacts/release-contract/report.json \
  --junit artifacts/release-contract/junit.xml
```

## Packaged target matrix

Every release produces and statically validates all six archives. Portable
execution is required where GitHub provides the corresponding native runner.

| Target | Archive | Pre-publication artifact validation | Native portable execution |
|---|---|---:|---:|
| Linux x64 | `omnideck-linux-amd64.tar.gz` | Required | Required |
| Linux ARM64 | `omnideck-linux-arm64.tar.gz` | Required | Required |
| macOS x64 | `omnideck-darwin-amd64.tar.gz` | Required | Not available on the hosted matrix |
| macOS ARM64 | `omnideck-darwin-arm64.tar.gz` | Required | Required |
| Windows x64 | `omnideck-windows-amd64.zip` | Required | Required |
| Windows ARM64 | `omnideck-windows-arm64.zip` | Required | Not available on the hosted matrix |

The `Release` workflow enforces the following sequence for a version tag:

1. Validate the SemVer tag and confirm its commit is already on `main`.
2. Run `make verify` against the tagged source.
3. Build all six targets and generate executable SBOMs and attestations.
4. Validate the final archive and architecture for all six targets.
5. Run the packaged portable contract on the four native hosted targets.
6. Pause at the protected `release` environment.
7. Generate `SHA256SUMS`, attest the release archives and checksum manifest,
   and publish the GitHub release after approval.

After publication, the manually dispatched `Test a published CLI release`
workflow downloads the public assets. It verifies the published checksum and
GitHub attestation for every archive, runs artifact mode for all six targets,
and runs portable mode for the four native hosted targets.

```sh
gh workflow run release-contract.yml \
  --ref main \
  -f version=v0.10.0-alpha.2
```

## Hardware lifecycle requirements

Podman is the release-gating container runtime. Docker mode exists only for
legacy and coexistence diagnostics and is not evidence that the production
runtime path passed.

The hardware harness uses unique `omnideck-hw-*` resources, high ports,
temporary configuration, a local fixture registry, and a fixture image. It
verifies explicit runtime selection, non-interactive setup, shared runtime and
instance configuration, web UI port mapping, status, logs, persistent volumes,
stop/start/restart, doctor output, removal, cleanup, and machine-readable
reports. It does not install a runtime or exercise interactive operating-system
permission flows.

Run it directly on a dedicated machine:

```sh
OMNIDECK_HARDWARE_ENGINE=podman ./tests/hardware/run.sh
```

```powershell
./tests/hardware/run.ps1 -Engine podman
```

The `Hardware lifecycle tests` workflow is manual and targets hardened
self-hosted Linux x64, Windows x64, and macOS ARM64 runners. It is not part of
`release.yml` and does not run when those runners are unavailable.

## Manual requirements

The checked-in procedures are the required source for behavior that cannot be
safely or reliably exercised on hosted runners:

- [Bare `omnideck` first run](tests/manual/first-run.md) verifies the real
  terminal-first setup, automatic recommended defaults, resource creation,
  dashboard routing, and second-launch behavior.
- [Windows clean-host installation](tests/manual/windows-clean-host.md)
  verifies clean installation, WSL/runtime setup, permission UI,
  restart/resume, repair, and preservation of persistent volumes.
- [Upgrade, backup, restore, and removal](tests/manual/upgrade-backup-restore.md)
  verifies a real stable-to-candidate upgrade and repair, persistent data,
  simultaneous instances, keep/delete choices, backup contents, restore, and
  cleanup. Run it independently on Windows x64, a supported native macOS
  architecture, and at least one supported mutable Linux x64 distribution.

Each execution must identify the release tag and binary SHA-256; host,
architecture, runtime, WSL, and operating-system versions; starting and final
resource inventories; commands and exit codes; observations; cleanup; and an
explicit pass, fail, or blocked result. Agents follow the same procedures and
must stop before altering resources that were not created for the test.

## Promotion gates

Promotion is based on evidence for the exact candidate commit and published
assets. A new tag creates a new build; an alpha is never renamed or mutated
into a beta or RC.

### Alpha

Before an alpha is published, it requires:

- required CI and CodeQL checks green on the exact commit;
- the tag accepted by the release source-verification gate;
- all six final archives passing artifact validation;
- all four native packaged targets passing the portable contract;
- successful checksum, SBOM, and provenance generation.

After publication, the published-release workflow must pass against the public
assets. A failure does not mutate or replace the immutable alpha; it leaves that
alpha unqualified as evidence for a later promotion until the issue is fixed in
a new release.

An alpha is cut for a meaningful testable increment, not for every merge.

### Beta

A beta requires every alpha gate plus recorded Podman hardware-lifecycle and
guided-setup evidence on every supported test machine available for the
candidate. Any unavailable machine is recorded as blocked coverage.

### Release candidate

An RC requires every beta gate plus recorded evidence for:

- bare `omnideck` first run from clean configuration;
- Windows clean-host installation and restart/resume when Windows is in scope;
- stable-to-candidate upgrade and repair from the supported prior release on
  Windows x64, a supported native macOS architecture, and at least one
  supported mutable Linux x64 distribution;
- persistent-volume preservation;
- backup and restore;
- multiple simultaneous instances;
- instance removal with both keep and delete choices;
- local Ollama connectivity where applicable; and
- Desktop/Tauri compatibility with JSON contract 2 and runtime schema 4,
  including valid status JSON accompanied by a nonzero process exit where the
  command reports a non-ready state.

All six targets must pass artifact validation. The four native hosted targets
must pass portable execution. The two hosted-native gaps must remain explicit
in the candidate evidence and may be supplemented by dedicated hardware.

### Stable

A stable release normally requires a selected RC with no unresolved release
blocker. Release notes must describe upgrade behavior and known limitations.

A prerelease may be promoted directly to stable without publishing an RC only
when every condition below is recorded and reviewed before the protected
`release` environment is approved:

- the qualifying prerelease passed every applicable beta and RC requirement,
  including the required manual platform evidence;
- the candidate differs from that prerelease only in documentation, test
  procedures, release policy, or retained evidence;
- Go source, dependencies, build inputs, release workflows, and packaged
  application behavior are unchanged;
- every remaining known issue is explicitly classified as non-blocking and is
  included in the release notes when user-visible; and
- the direct-promotion decision identifies the qualifying prerelease,
  candidate commit, intended stable version, evidence, and known limitations.

The stable tag still creates a new immutable build with new embedded version
metadata. Its source gate, all six artifact checks, four native portable
contracts, checksums, SBOMs, provenance, and post-publication public-asset
verification remain mandatory. Any product source, dependency, build-input, or
release-workflow change after the qualifying prerelease requires an RC and a
complete application of the applicable gates.

Beta may be skipped only when the candidate already satisfies every beta and RC
requirement.

## Evidence and retention

Automated evidence consists of immutable GitHub Actions run URLs and their
uploaded JSON/JUnit reports, SBOMs, checksums, and attestations. Local generated
reports belong under `artifacts/` and are not committed.

Hardware and manual evidence is attached to the candidate's promotion record.
The protected `release` environment is the pre-publication approval point:
approval means the reviewer has confirmed that all requirements applicable
before publication are present and passing. Post-publication checks complete
the release evidence. A failed or blocked required item prevents a later
promotion from using that release as qualifying evidence.
