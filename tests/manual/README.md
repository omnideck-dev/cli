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

- [Local VM lab controls and automated boundary](local-vm-lab.md)
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

Use the automated VM matrix for covered Linux and Windows behavior, and use the
lab only to provide the host for a remaining manual procedure. Create evidence
with `lab.sh artifact-path`/`evidence-init`, run every mutating command inside a
lease with `--cleanup-baseline clean`, and let that lease restore the disposable
overlay. Never use `snapshot`, `install-windows`, or `install-atomic` during an
ordinary release test. Routine `lab.sh cleanup` enforces retention; the
intentional `cleanup --all-generated --yes --apply` removes all generated lab
artifacts, caches, and retained reset state.
