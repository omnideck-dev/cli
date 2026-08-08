# Bare `omnideck` first-run test

## Purpose

Verify the actual terminal-first setup journey by invoking the released binary
with no arguments. This is a semantic and visual test; it is not replaced by
`omnideck setup`, JSON mode, or model-level TUI unit tests.

## Starting state and safety

- Use a disposable VM or dedicated machine with a ready Podman installation.
- Confirm there are no saved Omnideck instances for the test user.
- Record existing Podman machines, containers, volumes, and occupied ports.
- On a shared machine, stop: the bare flow can create default-named resources.

## Procedure

1. Verify the release checksum, provenance, and `--json --version` output.
2. Start a transcript or screen recording.
3. Run the downloaded executable with exactly zero arguments.
4. Confirm the welcome screen explains the one-time setup and Enter begins it.
5. Confirm the visible phases progress through computer setup, secure space on
   Windows/macOS, application files, and final checks without exposing raw
   internal output as normal UI.
6. Confirm recommended settings appear. Accept them with Enter.
7. Confirm the review accurately describes the instance, runtime, port, memory,
   volumes, and image. Continue with Enter.
8. Confirm setup reaches a clear ready state and the loopback application URL
   returns a successful response.
9. Exit normally and run the same bare executable again. Confirm it opens the
   dashboard rather than creating another instance.
10. Run `--json list`, `--json runtime status`, and named `--json status`.
    Validate JSON contract 2 and runtime schema 4.
11. Confirm exactly one intended instance and its two named volumes were added;
    all resources from the starting inventory must remain unchanged.
12. Remove the test instance through the documented removal flow, verify the
    selected volume policy, and revert the VM or clean only recorded test IDs.

## Pass criteria

The first launch completes, the second launch routes to the dashboard, the web
application is reachable, persisted state matches the UI, and no unrelated
resource changes. Record visual/copy problems as failures or release issues;
do not normalize them away as spinner noise.
