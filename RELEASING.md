# Releasing Omnideck CLI

This file is the authoritative release and promotion checklist. Test-suite
implementation details live in [TESTING.md](TESTING.md), while procedures that
require a person or agent live in [tests/manual](tests/manual/README.md).

Omnideck CLI uses Semantic Versioning and GitHub prereleases. A version suffix
selects the release channel automatically:

| Version | Channel | GitHub behavior |
|---|---|---|
| `v0.8.0-alpha.1` | Early preview; behavior and UX may change | Prerelease |
| `v0.8.0-beta.1` | Feature-complete; wider platform testing | Prerelease |
| `v0.8.0-rc.1` | Stable candidate; release blockers only | Prerelease |
| `v0.8.0` | Supported public release | Latest release |

This is the same broad pattern used by many open-source projects: development
happens on the main branch, immutable version tags identify builds, prerelease
channels collect increasingly broad feedback, and only a stable version becomes
the default release.

## Publish a preview

1. Run `make verify` locally.
2. Merge the intended changes to `main` and ensure every required CI and CodeQL
   check is green.
3. Choose the next prerelease identifier. Increment the final number for every
   new build; never move or replace a published tag.
4. Create and push an annotated tag:

   ```sh
   git switch main
   git pull --ff-only
   git tag -a v0.8.0-alpha.1 -m "Omnideck CLI v0.8.0-alpha.1"
   git push origin v0.8.0-alpha.1
   ```
5. Open the release workflow. Confirm that the source checks, vulnerability
   scan, builds, SBOM generation, and provenance attestations passed. Approve
   the protected `release` environment only after reviewing those results.

The release workflow rejects malformed tags and tags that do not point to a
commit already merged into `main`. It then repeats the source and vulnerability
checks, builds all supported platform archives, creates `SHA256SUMS`, SBOMs and
provenance attestations, and pauses for approval before publishing one GitHub
release. Tags containing a suffix are marked as prereleases and do not replace
the latest stable release.

## Promotion

Promotion creates a new immutable tag and release. Never rename an alpha into
an RC, replace its assets, move a published tag, or retag a different commit.
An RC may use the same source commit as a tested alpha, but it is still rebuilt
with new embedded version metadata and therefore has different binary hashes.

Fix issues on `main`, then publish the next identifier:

```text
v0.8.0-alpha.1 → v0.8.0-alpha.2 → v0.8.0-beta.1 → v0.8.0-rc.1 → v0.8.0
```

Recommended gates:

- Alpha: hosted CI and packaged portable-contract checks pass; the release is a
  meaningful testable increment rather than an artifact from every merge.
- Beta: alpha gates plus Podman lifecycle and guided setup evidence across the
  available supported-platform matrix.
- RC: beta gates plus first-run, restart/resume where applicable, update,
  rollback, backup/restore, multi-instance, and removal evidence. All six
  packaged targets must pass static validation, and native execution gaps must
  be recorded explicitly.
- Stable: the selected RC has no unresolved release blockers and its notes call
  out upgrades and known issues. Any code change requires another RC.

Beta may be skipped only when the candidate has already satisfied every beta
and RC gate. Promotion is based on evidence, not time spent in a channel.

## Candidate-to-release checklist

Before creating an RC tag, record the candidate commit, source alpha, intended
RC version, and links or attachments for every applicable item below:

- [ ] Hosted CI is green on the exact candidate commit.
- [ ] The published alpha passes the portable release contract.
- [ ] All six archives have the expected format, architecture, checksum, SBOM,
      and provenance.
- [ ] Podman lifecycle tests passed on every machine presently available.
- [ ] Bare `omnideck` first-run completed from a clean configuration.
- [ ] Windows installation and restart/resume were checked, or the native gap
      is explicitly recorded.
- [ ] Upgrade, persistent-volume, backup/restore, multi-instance, removal, and
      Ollama scenarios were checked as applicable.
- [ ] Desktop/Tauri consumers accept JSON contract 2 and runtime schema 4,
      including valid status JSON accompanied by a nonzero process exit.
- [ ] Known issues were reviewed and none is an unresolved release blocker.

The protected `release` environment is the enforcement point while clean-host
and hardware testing is manual. Do not approve its publication step until the
checklist evidence has been reviewed.

After the tag is pushed, the release workflow repeats source verification,
builds all six targets, statically validates every binary, and executes the
packaged portable contract on native GitHub-hosted Windows x64, Linux x64,
Linux ARM64, and macOS ARM64 runners. Publication depends on those jobs. After
publication, download the public assets once more and verify their checksums,
provenance, embedded version, and portable contract.

If any RC check fails after publication, fix forward on `main` and publish the
next RC number. Do not replace the failed RC.

GitHub-generated notes are a useful baseline. Curate the release description for
user-visible changes, upgrade notes, known limitations, and a short request for
preview feedback.
