# Setup flow matrix

The interactive CLI has four user journeys. They share runtime diagnosis, but
they do not share the same destination.

| Journey | Starts when | Runtime is ready | Runtime needs attention |
| --- | --- | --- | --- |
| First setup | No Omnideck instances exist | Continue to recommended instance settings | Let the user choose Docker or Podman, guide setup, then continue |
| Returning, working | One or more instances exist and their saved runtime is ready | Open the dashboard | Not applicable |
| Returning, runtime broken | Instances exist but their saved runtime is not ready | Return to the dashboard | Repair only the saved shared runtime; never create an instance or switch runtimes |
| Add instance | The user chooses **Setup** from the dashboard | Continue to a unique name, port, and saved space | Repair the saved shared runtime first, then continue |

Doctor is a separate diagnostic journey. It must use the same runtime probe
states and setup plans as Setup so it cannot give conflicting instructions.

## Runtime setup stages

Runtime setup has one mutually exclusive stage at a time:

1. **Choose** — show both usable choices on first setup, or the one saved choice
   for repair and additional instances.
2. **Review** — explain exactly what Omnideck will run or open. Nothing changes
   before the second confirmation.
3. **Working** — run the reviewed command or recheck runtime readiness.
4. **Waiting** — an official installer, Store listing, or help page is open;
   explain what to finish and make **check again** available.

Every failed recheck returns to **Choose** with a state-specific next step. Even
if a probe result is incomplete and no safe plan can be built, **R** and
**Enter** remain available to check again.

## Primary platform choices

| Platform | Recommended | Alternative | Missing-runtime setup |
| --- | --- | --- | --- |
| Windows x64 | Docker Desktop | Podman | Microsoft Store for Docker; official `.msi` for Podman |
| Windows ARM | Docker Desktop | Podman | Docker's official ARM instructions; official ARM `.msi` for Podman |
| Apple-chip Mac | Podman | Docker Desktop | Official Podman `.pkg`; Docker's official Mac instructions |
| Intel Mac | Docker Desktop | Podman with limitations | Docker's official Mac instructions; current Podman guidance |
| Linux | Podman | Docker | Native package manager for known Podman distributions; official Docker instructions |
| Linux inside Windows (WSL) | Docker Desktop | Podman | Windows Microsoft Store plus WSL integration; Linux Podman path remains available |

For each runtime, the same flow also covers installed-but-stopped,
Podman-machine-missing, Podman-machine-stopped, account-access, unsupported
Docker version, and installed-but-broken states. Setup-plan tests require every
supported state to have plain-language steps and either a reviewed command or
an official page.
