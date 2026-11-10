---
type: fixed
area: automation
---

`omnideck environment ensure` and `omnideck repair` apply a port saved with
`config set` safely, reporting `PORT_IN_USE` without touching the container
when another app holds it instead of waiting for the application on the wrong
port. `omnideck remove` no longer deletes a container or saved data that
another saved installation still uses.
