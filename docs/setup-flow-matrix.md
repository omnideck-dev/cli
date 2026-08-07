# Setup flow matrix

The Desktop setup is the canonical experience. The interactive CLI adapts the
same copy, phase order, progress, permission guidance, restart behavior, and
failure next steps to a terminal. A user starts it by running bare `omnideck`;
`runtime ensure` is an internal Desktop/automation boundary, not a user step.

| Journey | Bare-command detection | Destination |
| --- | --- | --- |
| First setup | No saved OmniDeck instances | Welcome, automatic prerequisites and runtime, recommended instance defaults, application download, final checks |
| Returning, working | At least one instance and Podman are ready | Control-plane dashboard |
| Returning, runtime broken | Saved instances exist but Podman is not ready | Automatic runtime repair, then the dashboard; never create another instance |
| Returning, instance broken | Podman is ready but a saved container is missing | Doctor on that instance, with its existing volumes preserved |
| Add instance | User chooses Setup from the dashboard | Reuse/repair shared Podman, then show instance settings and review |

## First-run phases

Only one activity is shown at a time:

1. **Computer setup** — `Getting your computer ready…`
2. **Secure space** — `Preparing a secure space to run in…` on Windows and macOS
3. **Application files** — `Downloading omnideck’s files…`
4. **Final checks** — `Almost ready…`

The phase weights match Desktop: 25%, 15%, 50%, and 10%. Linux omits the
secure-space phase and renormalizes the remaining weights. Permission screens
are the only place setup names WSL or Podman, because informed consent requires
the user to know what the operating-system prompt will change.

There is no Podman/Docker choice, numbered component walkthrough, browser
download, or “return and check again” screen. Setup downloads and verifies the
pinned installer itself, invokes the native installer, and continues when it
finishes.

## Platform behavior

| Platform | Computer setup | Secure space |
| --- | --- | --- |
| Windows x64/ARM64 | Detect or enable WSL 2; download, checksum-check, Authenticode-check, and install pinned Podman | Create or repair `omnideck-runtime` with WSL, rootless mode, and user-mode networking |
| Apple-chip macOS | Download, checksum-check, package-signature-check, and install pinned Podman | Create or repair `omnideck-runtime`; Podman chooses CPU and sparse-disk defaults while Omnideck sets a compatible memory ceiling |
| Intel macOS | Download, checksum-check, package-signature-check, and install the newest official Intel Podman package | Create or repair `omnideck-runtime` with the same memory-only OmniDeck policy |
| Linux | Install Podman through the recognized native package family, using direct root execution, PolicyKit, or sudo as appropriate | Not applicable; containers run directly |

Windows setup reports a typed restart requirement when WSL cannot finish until
reboot. **Restart now** creates a one-time per-user resume entry, restarts
without forcing applications closed, and relaunches bare `omnideck` after
sign-in. **Restart later** exits without scheduling a restart.

## Shared resource defaults

The CLI calculates these values once and returns them in runtime-contract
schema 4. The TUI uses the policy directly; Desktop uses the values returned by
its bundled CLI. The CLI alone creates and updates `desktop.yaml` as part of
the same transaction that reconciles the Desktop container.

| Host RAM | Windows/Linux container | macOS container | macOS VM ceiling | Shared memory |
| --- | --- | --- | --- | --- |
| Under 6 GB | 1 GB | 1 GB | 4 GB | 50% of the container limit |
| 6–11 GB | 2 GB | 2 GB | 4 GB | 50% of the container limit |
| 12–23 GB | 3 GB | 3 GB | 5 GB | 50% of the container limit |
| 24–47 GB | 4 GB | 4 GB | 6 GB | 50% of the container limit |
| 48 GB or more | 6 GB | 4 GB | 6 GB | 50% of the container limit |

Containers have no CPU quota and named volumes have no artificial disk quota.
On Windows, Podman machine CPU, memory, and disk limits originate from WSL and
the user's `.wslconfig`; OmniDeck does not replace them. Every default Windows
container tier fits within WSL's default half-of-host memory ceiling with guest
headroom. On Linux there is no machine layer. On macOS, Podman chooses its
platform-aware CPU count and sparse-disk ceiling. OmniDeck sets only VM memory:
the container limit plus 2 GB for Podman and the guest OS, with a 4 GB minimum.
Desktop validates this relationship before asking the CLI to reconcile the
container.

## Failure contract

Every timeout or failure includes a plain-language cause and next step. Shared
runtime failures are categorized as component, permission, download,
environment, restart, or unsupported-system issues. Both Desktop and TUI
consume that classification instead of parsing command output independently.

Doctor remains a separate diagnostic journey, but runtime repair returns to
this same automatic setup backend so it cannot drift from first-run behavior.
