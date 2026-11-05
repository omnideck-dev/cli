---
type: fixed
area: automation
---

JSON output now names instances consistently. The `name` in `status`, `start`,
`stop`, `restart`, `wait`, `add`, `update`, and `environment ensure` results is
the instance name that `--name` accepts, matching `list`. `environment ensure`
keeps an existing instance's container even when its name differs, and
`update --json` reports a busy port or failed download with the same error codes
as `add`.

`omnideck add` and new-instance setup no longer reuse the name of an existing
instance whose container is named differently, which could overwrite that
instance's saved settings.
