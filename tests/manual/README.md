# Manual and agent-operated release tests

These procedures cover behavior that is unsafe or impractical on ephemeral
GitHub-hosted runners. Run them on a disposable VM or dedicated test machine,
never on a machine containing the only copy of important Omnideck data.

Every completed procedure must record:

- release tag, binary SHA-256, operating system, architecture, and CLI version;
- Podman, WSL, and relevant operating-system versions;
- starting-state and final-state machine/container/volume inventories;
- commands, exit codes, visual observations, and observed deviations;
- cleanup or VM-revert result; and
- an explicit pass, fail, or blocked conclusion.

Keep the evidence compact. Capture a screenshot or recording only when it is
needed to prove or diagnose visual behavior, and remove copied release assets,
VM overlays, backup archives, and other bulky intermediates after recording
their required hashes and assertions. Before sharing or committing a report,
remove personal usernames, home directories, hostnames, external IP addresses,
tokens, personal SSH material, and machine-specific paths.

Available procedures:

- [First run with bare `omnideck`](first-run.md)
- [Windows clean-host installation and restart/resume](windows-clean-host.md)
- [Stable upgrade, backup, restore, and removal](upgrade-backup-restore.md), run
  independently on Windows, macOS, and at least one supported Linux
  distribution

An agent may execute these files verbatim. It must stop before any action that
could alter resources not created by the test and must report unavailable
hardware as a coverage gap rather than treating it as a pass.

## Using the external local VM lab

If the workstation has the optional Omnideck release VM lab, follow the
[local disposable VM lab workflow](../../TESTING.md#local-disposable-vm-lab).
It provides revertible Ubuntu, Debian, Fedora, Silverblue, and Windows guests
without changing the development host. The VM identifiers are lab roles, not
CLI package formats. The lab does not provide macOS coverage, and Silverblue's
stock Podman installation does not satisfy a Podman-absent starting condition.

Use the lab only to provide the host described by a procedure; the steps and
pass criteria in these checked-in files remain authoritative. End each run by
copying out its compact result, stopping the guest, and resetting its disposable
overlay. Do not use the lab's `snapshot` or installer commands during an
ordinary release test.
