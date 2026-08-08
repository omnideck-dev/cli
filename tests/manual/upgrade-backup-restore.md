# Upgrade, backup, restore, and removal test

## Starting state and safety

Use a disposable machine containing one instance created by the previous
supported CLI. Write unique sentinel files to both persistent volumes and
record their hashes, permissions, container image, port, and configuration.

## Procedure

1. Verify and run the candidate release's `--json --version`.
2. Run the documented update flow and capture every NDJSON event or TUI screen.
3. Confirm the new image is active and both sentinel files are unchanged.
4. Stop/start and recreate or repair the instance; confirm persistence again.
5. Create a second uniquely named instance on another port and confirm both web
   applications and named commands address the correct instance.
6. Remove the second instance while keeping volumes; confirm only its container
   and configuration are removed.
7. Recreate or repair it and confirm the kept data returns.
8. Remove it again with backup and explicit volume deletion. Inspect the outer
   archive and both nested volume exports, and confirm the original resources
   are gone.
9. Restore the backup into newly created test volumes, attach them to an
   isolated fixture container, and verify sentinel hashes and permissions.
10. Remove only resources recorded as part of this procedure and save before/
    after inventories with the result.

## Pass criteria

Upgrade and recreation preserve data, named commands remain isolated,
keep/delete choices affect only the selected instance, and a generated backup
can actually be restored—not merely listed.
