---
type: added
area: startup
---

Start and restart wait for the app to respond before reporting success. The new
`omnideck wait` command checks readiness without changing an installation.
Supported app versions report settings-upgrade failures with retry guidance;
existing apps continue to use HTTP readiness checks. JSON output advances to
contract version 4 with specific startup error codes.
