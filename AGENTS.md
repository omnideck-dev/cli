# Repository guide

- Run `make verify` and focused tests before handoff; also run `make race` for concurrency changes.
- Route Podman operations through `engine.Engine` and shared workflows, and preserve the platform supplied by `engine.RunOptions` or `HostPlatform`.
- Run lifecycle and TUI tests in the disposable VM lab, never on the developer host. Set `OMNIDECK_VM_LAB_DIR=/mnt/data/VMs/omnideck-release-lab`; see `TESTING.md` and `tests/e2e/README.md` for `make vm-e2e` and `make vm-e2e-matrix`.
