---
type: changed
area: automation
---

`omnideck remove` accepts `--name` like every other instance command, in
addition to the instance name as an argument. Under `--json`, a missing name,
or two different names, returns `MISSING_REQUIRED_FLAG` instead of
`INTERNAL_ERROR`.
