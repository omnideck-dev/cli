---
type: added
area: automation
---

`omnideck repair` recreates a missing container from its saved settings and
the Omnideck version already downloaded, reusing the saved data volumes, and
Doctor now suggests it. Repair in the interactive Doctor uses the same steps
and no longer downloads a new version or creates empty saved data. `omnideck remove --backup-dir` saves the backup in a folder you
choose. Doctor offers a one-step update when the MCP sign-in address is
missing or wrong.

JSON mode is now contract 5. Rejected settings and flag values return
`INVALID_SETTING` with the setting's name, missing saved data returns
`MISSING_STORAGE`, and `start` or `restart` on a missing container returns
`CONTAINER_NOT_FOUND` with a repair action when its data is intact.
