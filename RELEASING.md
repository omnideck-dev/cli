# Releasing Omnideck CLI

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

Do not rebuild an existing version or retag a different commit. Fix issues on
`main`, then publish the next identifier:

```text
v0.8.0-alpha.1 → v0.8.0-alpha.2 → v0.8.0-beta.1 → v0.8.0-rc.1 → v0.8.0
```

Recommended gates:

- Alpha: unit tests pass and the guided setup renders on every target OS.
- Beta: runtime setup has been exercised on the supported platform matrix.
- RC: installation, update, rollback, and uninstall pass end-to-end.
- Stable: no unresolved release blockers; notes call out upgrades and known issues.

GitHub-generated notes are a useful baseline. Curate the release description for
user-visible changes, upgrade notes, known limitations, and a short request for
preview feedback.
