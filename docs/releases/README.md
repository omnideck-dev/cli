# Checked-in CLI release notes

Each release stores its reviewed GitHub release body here as
`v<version>.md`. Generate the first draft from outstanding fragments:

```sh
node scripts/release-notes.mjs generate \
  --version v1.2.3 \
  --output docs/releases/v1.2.3.md
```

Curate that draft, commit it with the release change, and remove the fragments
whose text it incorporates. The tag workflow requires the exact file and
publishes it as the GitHub release body.
