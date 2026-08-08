# Manual and agent-operated release tests

These procedures cover behavior that is unsafe or impractical on ephemeral
GitHub-hosted runners. Run them on a disposable VM or dedicated test machine,
never on a machine containing the only copy of important Omnideck data.

Every completed procedure must record:

- release tag, binary SHA-256, operating system, architecture, and CLI version;
- Podman, WSL, and relevant operating-system versions;
- starting-state and final-state machine/container/volume inventories;
- commands, exit codes, screenshots or recordings, and observed deviations;
- cleanup or VM-revert result; and
- an explicit pass, fail, or blocked conclusion.

Available procedures:

- [First run with bare `omnideck`](first-run.md)
- [Windows clean-host installation and restart/resume](windows-clean-host.md)
- [Upgrade, backup, restore, and removal](upgrade-backup-restore.md)

An agent may execute these files verbatim. It must stop before any action that
could alter resources not created by the test and must report unavailable
hardware as a coverage gap rather than treating it as a pass.
