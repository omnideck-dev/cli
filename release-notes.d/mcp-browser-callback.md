---
type: added
area: integrations
---

The CLI now supplies the application's local browser address automatically,
allowing compatible omnideck builds to complete MCP integration sign-in on
the computer running omnideck, including when a custom port is used. Existing
containers are recreated once when their configuration is reconciled; saved
data volumes are preserved. This does not add support for signing in from a
browser on a different computer.
