# Release security

Published omnideck release archives are built by the tag-triggered GitHub Actions
workflow; manual runs produce unpublished proof artifacts. The workflow tests
the source, builds each supported platform,
creates checksums and SPDX software bills of materials (SBOMs), records GitHub
artifact attestations, and then pauses for a maintainer to approve publication.

The tag must resolve to a commit already on `main`. Formatting, dependency
metadata, static analysis, workflow validation, tests, and Go's reachable-code
vulnerability scan must all pass before any platform build can be published.

## What a downloader can verify

`SHA256SUMS` records the exact bytes published for every archive and SBOM. A
different hash means the downloaded file is not the published file and must not
be used.

GitHub attestations connect the archive and extracted executable to the source
repository, commit, tag event, and release workflow that built them:

```sh
gh attestation verify omnideck.exe --repo omnideck-dev/cli
```

An attestation establishes origin and integrity. It is not a malware scan or a
Microsoft Windows publisher signature.

## If Microsoft Defender reports a release

Do not restore, allow, or execute the file while the report is unresolved. Also
do not rename, repack, or modify the binary merely to avoid the detection.

Record the archive and executable hashes and the local Defender versions:

```powershell
Get-FileHash .\omnideck-windows-amd64.zip -Algorithm SHA256
Get-FileHash .\omnideck.exe -Algorithm SHA256
Get-MpComputerStatus |
  Select-Object AMEngineVersion, AntivirusSignatureVersion, AntivirusSignatureLastUpdated
```

Submit the specific extracted executable to the
[Microsoft Security Intelligence portal](https://www.microsoft.com/en-us/wdsi/filesubmission):

1. Choose **Software developer**.
2. Choose the Microsoft Defender product that reported it.
3. Choose **Incorrectly detected as malware/malicious**.
4. Enter the exact detection name and Defender definition version.
5. Include the SHA-256 values, release URL, commit, workflow run, software
   purpose, and affected Windows version.

Keep the Microsoft submission ID with the release incident. If Microsoft finds
the file clean, wait for the corrected security intelligence, update Defender,
and test a fresh download on a clean Windows machine before restoring normal
distribution.

## Maintainer release rules

- Merge release changes before creating a version tag. Never build a release
  from an unreviewed local branch. The workflow enforces that the tag is on
  `main`.
- Review the completed build and security jobs before approving the protected
  `release` environment. A pushed tag alone does not publish files.
- Never replace an asset under an existing version. Publish a new prerelease so
  hashes, provenance, and user reports remain unambiguous.
- Keep every GitHub Action pinned to a full commit SHA. Dependabot proposes
  reviewed updates to those pins.
- The build timestamp comes from the source commit, and Go builds use
  `-trimpath`; the unsigned Windows executable can therefore be reproduced
  byte-for-byte from the same source and build inputs.
- Preserve Go symbol and module metadata so users and security analysts can
  inspect the executable with `go version -m` and standard binary tools.

## Application image update channel

The CLI intentionally uses `ghcr.io/omnideck-dev/omnideck:latest`. Setup and
Update therefore fetch the newest published application image rather than one
pinned to the CLI version. This keeps early releases on one simple update
channel, but the CLI's checks cannot guarantee the contents of an image that is
published later under that moving tag. The Omnideck application repository must
eventually apply equivalent dependency scanning, image scanning, provenance,
and protected publishing to that channel.

## Remaining Windows publisher signing work

Windows preview executables currently include product/version resources but are
not Authenticode signed. Omnideck will continue relying on reproducible builds,
checksums, SBOMs, attestations, and malware-report review while the project is
unsigned. If the project later qualifies for the free
[SignPath Foundation](https://signpath.org/) open-source program, the release
workflow should:

1. Build and attest the reproducible unsigned Windows executables.
2. Send only tag builds from the protected GitHub `release` environment to the
   approved SignPath project.
3. Sign both Windows architectures with SHA-256 and an RFC 3161 timestamp.
4. Run `signtool verify /pa /v` before packaging or publishing anything.
5. Attest and checksum the final signed archives.

The repository must never store a `.pfx` file or certificate password. Signing
changes the executable bytes, so the unsigned pre-signing build remains the
reproducible artifact while provenance, the Authenticode signature, and the
timestamp protect the published result.

## macOS Developer ID signing

Intel and Apple Silicon release builds run on macOS and require the protected
`desktop-signing` environment. Only `main` and `v*` tags may use it, with Larry's
approval. Other platform jobs use the unprivileged `release-build` environment.
The certificate archive must include its private key and Apple Developer ID
intermediate certificate. Five environment secrets configure the ephemeral
signing keychain and Apple notary authentication:

- `DESKTOP_MAC_CERTIFICATE_P12_BASE64`
- `DESKTOP_MAC_CERTIFICATE_PASSWORD`
- `DESKTOP_APPLE_API_KEY_ID`
- `DESKTOP_APPLE_API_ISSUER_ID`
- `DESKTOP_APPLE_API_PRIVATE_KEY_BASE64`

The scripts require Developer ID Application for team `2FL6BUG8Q4`, hardened
runtime, an Apple timestamp, and an Accepted notarization response. Signing
happens before executable SBOM generation, attestations, archive creation, and
checksums, so all integrity records refer to the final signed bytes. Temporary
signing material is removed even when a build fails. Notarization waits up to
ninety minutes. Failed Mac jobs retain the executable in a separate recovery
artifact, allowing a maintainer to inspect the existing submission and verify
its ticket later without re-signing. Recovery artifacts are not release assets
and must pass the same trust checks before any distribution.

Standalone executables and ZIP submissions cannot have notarization tickets
stapled to them. Apple's ticket is checked online by `codesign
--check-notarization -R "=notarized"`; first-use Gatekeeper verification can require network
access. The desktop's bundled CLI is independently signed within the desktop
app and covered by its notarized, stapled distribution.

Before tagging a release, dispatch `release.yml` from `main` to build and verify
`signing-proof` artifacts without publication. Tag releases remain immutable;
existing unsigned releases are not overwritten. Signing does not establish
Windows publisher trust.
