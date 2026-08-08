# Windows clean-host and restart/resume test

## Starting state and safety

Use a revertible Windows VM snapshot with no OmniDeck configuration, Podman,
OmniDeck containers or volumes, and with the intended WSL starting state. Save
the snapshot identifier and initial `wsl --status`, installed-application, port,
scheduled-startup, and Podman inventories.

Do not run this procedure on the preserved Desktop regression fixture or a
daily-use Windows installation.

## Procedure

1. Download and verify the exact Windows release asset.
2. Run bare `omnideck` and confirm the welcome screen.
3. Start setup and confirm permission/UAC screens explain what will change
   before Windows presents an operating-system prompt.
4. If a restart is required, first choose **Restart later**. Confirm the CLI
   exits without restarting or installing a resume entry.
5. Repeat setup and choose **Restart now**. Confirm Windows is not force-restarted
   over other applications and that one per-user resume entry is created.
6. Sign in after restart. Confirm bare setup resumes once, removes or consumes
   the one-time entry, and does not create duplicate machines or instances.
7. Complete recommended setup and validate the loopback application response.
8. Relaunch bare `omnideck`; confirm it opens the dashboard.
9. Stop the Podman machine and relaunch. Confirm runtime repair is offered and
   no second application instance is created.
10. Remove only the application container while preserving both volumes, then
    relaunch. Confirm Doctor/repair is selected and the volumes remain intact.
11. Save final inventories, a compact transcript, the pass/fail result, and
    only the screenshots needed to prove or diagnose visual behavior; then
    revert the VM snapshot.

## Pass criteria

Installation, consent, restart, one-time resume, completion, returning launch,
and recovery routing work without duplicate or unrelated resources. Any manual
repair required outside the displayed instructions is a failure.
