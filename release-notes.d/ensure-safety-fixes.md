---
type: fixed
area: automation
---

`omnideck environment ensure` no longer reuses the container or saved data of
another Omnideck installation when a new instance's name matches it, and never
suggests removing that installation's container. It also accepts shared memory
settings saved by earlier versions, leaves a port change saved with
`config set` for `update` to apply, and, under `--json`, reports the wait for
the application as its own `startup` stage.
