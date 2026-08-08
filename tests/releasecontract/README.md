# Release contract tests

This package treats an Omnideck CLI executable as a black box. It does not
import CLI implementation packages or build the binary under test.

`artifact` mode checks the executable format and target architecture without
running it. `portable` mode also executes non-destructive commands with closed
stdin, a temporary `OMNIDECK_CONFIG_DIR`, bounded timeouts, and separate
stdout/stderr capture. Portable mode compiles the checked-in schemas under
`contracts/` and validates real CLI output against them.

```sh
go run ./tests/releasecontract \
  --archive dist/omnideck-linux-amd64.tar.gz \
  --mode portable \
  --expected-version v0.10.0-rc.1 \
  --expected-os linux \
  --expected-arch amd64 \
  --report artifacts/release-contract/report.json \
  --junit artifacts/release-contract/junit.xml
```

To test an already extracted development binary, replace `--archive` with
`--binary`. Portable tests never invoke Podman or change the user's normal
configuration. Real-runtime coverage remains in `tests/hardware`; interactive
first-run and operating-system setup coverage remains in `tests/manual`.
