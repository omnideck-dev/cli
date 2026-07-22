# Security policy

## Supported versions

Security fixes are made against the latest release. Alpha, beta, and release
candidate builds are previews and may be replaced quickly, but please report
security problems in previews as well.

## Report a vulnerability privately

Do not open a public issue for a suspected vulnerability. Use GitHub's
[private vulnerability reporting form](https://github.com/omnideck-dev/cli/security/advisories/new)
so details are visible only to the maintainers until a fix is available.

Please include:

- the Omnideck CLI version and operating system;
- whether Docker or Podman was used;
- the affected command or setup screen;
- reproduction steps and the security impact;
- relevant logs with API keys, tokens, personal paths, and private data removed.

We aim to acknowledge reports within three business days. We will investigate,
coordinate a fix and disclosure with the reporter, and credit the reporter when
requested. Please do not access other people's data or disrupt services while
testing.

Reports about the application running inside the container belong in the
[Omnideck application repository](https://github.com/omnideck-dev/omnideck/security/advisories/new).

## Release safeguards

Pull requests run tests on supported operating systems, static analysis,
CodeQL, dependency review, and Go's vulnerability scanner. Releases repeat the
critical source and vulnerability checks, require the tagged commit to already
be on `main`, and pause for approval before GitHub publishes any assets.
Published artifacts include checksums, SBOMs, and GitHub build-provenance
attestations.
