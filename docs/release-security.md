# Release security

Omnideck release archives are built only by the tag-triggered GitHub Actions
workflow. The workflow tests the source, cross-builds each supported platform,
creates checksums and SPDX software bills of materials (SBOMs), records GitHub
artifact attestations, and then publishes the release.

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
  from an unreviewed local branch.
- Never replace an asset under an existing version. Publish a new prerelease so
  hashes, provenance, and user reports remain unambiguous.
- Keep every GitHub Action pinned to a full commit SHA. Dependabot proposes
  reviewed updates to those pins.
- The build timestamp comes from the source commit, and Go builds use
  `-trimpath`; the unsigned Windows executable can therefore be reproduced
  byte-for-byte from the same source and build inputs.
- Preserve Go symbol and module metadata so users and security analysts can
  inspect the executable with `go version -m` and standard binary tools.

## Remaining Windows publisher signing work

Windows preview executables currently include product/version resources but are
not Authenticode signed. The next trust step is a Public Trust certificate in
[Azure Artifact Signing](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options).
Once the Omnideck publisher identity is validated, the release workflow should:

1. Build and attest the reproducible unsigned Windows executables.
2. Send only tag builds from a protected GitHub `release` environment to an
   Azure-hosted Windows signing job using OpenID Connect.
3. Sign both Windows architectures with SHA-256 and an RFC 3161 timestamp.
4. Run `signtool verify /pa /v` before packaging or publishing anything.
5. Attest and checksum the final signed archives.

The certificate remains in Azure; the repository should never store a `.pfx`
file or a certificate password. Signing changes the executable bytes, so the
unsigned pre-signing build remains the reproducible artifact while provenance,
the Authenticode signature, and the timestamp protect the published result.
