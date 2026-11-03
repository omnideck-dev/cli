"""Execute the real cleanup functions with isolated lab/engine command fakes."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


HERE = Path(__file__).resolve().parent


class CleanupOwnershipTest(unittest.TestCase):
    def test_child_stops_and_records_result_but_leaves_reset_to_parent(self):
        for filename in ("run.sh", "run-windows.sh"):
            for status, keep, stop_failure in ((0, False, False), (7, False, False), (0, True, False), (7, True, False), (0, False, True)):
                with self.subTest(filename=filename, status=status, keep=keep, stop_failure=stop_failure):
                    text = (HERE / filename).read_text()
                    cleanup = "cleanup() {\n" + text.split("cleanup() {\n", 1)[1].split("\ntrap cleanup EXIT", 1)[0]
                    with tempfile.TemporaryDirectory(dir=HERE) as temporary:
                        root = Path(temporary)
                        recorder = root / "record-command"
                        recorder.write_text(
                            "#!/usr/bin/env python3\n"
                            "import json,os,sys\n"
                            "from pathlib import Path\n"
                            "with open(os.environ['COMMAND_LOG'],'a') as f: f.write(json.dumps([Path(sys.argv[0]).name,*sys.argv[1:]])+'\\n')\n"
                            "if sys.argv[1:3]==['container','inspect']: print('[]')\n"
                            "if sys.argv[1:2]==['stop'] and os.environ.get('FAIL_STOP')=='1': sys.exit(1)\n"
                        )
                        recorder.chmod(0o755)
                        (root / "lab.sh").symlink_to(recorder)
                        (root / "docker").symlink_to(recorder)
                        log = root / "commands.jsonl"
                        env = {**os.environ, "PATH": f"{root}:{os.environ['PATH']}", "COMMAND_LOG": str(log), "FAIL_STOP": str(int(stop_failure)), "FIXTURE_ROOT": str(root), "KEEP_VM": str(int(keep))}
                        script = """set -Eeuo pipefail
lab_dir="$FIXTURE_ROOT"; output_dir="$FIXTURE_ROOT"; vm=appimage
remote_staged=1; vm_started=1; keep_vm="$KEEP_VM"; registry_started=1
registry_name=owned-registry; fixture_local=owned-fixture; fixture_host=owned-host-fixture
remote_root=owned-remote-root; tls_pid=''; bridge_port=50001; firewall_rule=owned-firewall
baseline=runtime-ready
""" + cleanup + f"\ntrap cleanup EXIT\nexit {status}\n"
                        result = subprocess.run(["bash", "-c", script], env=env, capture_output=True, text=True)
                        expected_status = 1 if stop_failure else status
                        self.assertEqual(result.returncode, expected_status, result.stderr)
                        commands = [json.loads(line) for line in log.read_text().splitlines()]
                        lab = [row[1:] for row in commands if row[0] == "lab.sh"]
                        self.assertEqual(sum(row[0] == "stop" for row in lab), 1)
                        self.assertFalse(any(row[0] == "reset" for row in lab), lab)
                        self.assertIn(["evidence-finish", str(root), "passed" if expected_status == 0 else "failed"], lab)
                        self.assertIn(["docker", "rm", "-f", "--volumes", "owned-registry"], commands)
                        self.assertTrue((root / "registry-container-before-removal.json").exists())
                        remote_deletions = [row for row in lab if row[0] == "run" and any("Remove-Item" in arg or "rm -rf" in arg for arg in row)]
                        self.assertEqual(bool(remote_deletions), not keep)

    def test_parent_requests_one_cleanup_baseline_and_honors_keep_state(self):
        for filename in ("run.sh", "run-windows.sh"):
            text = (HERE / filename).read_text()
            lease = "  lease_args=" + text.split("  lease_args=", 1)[1].split("  lease_status=0", 1)[0]
            for keep in (False, True):
                with self.subTest(filename=filename, keep=keep):
                    setup = f"vm=appimage; lease_run_id=audit; baseline=runtime-ready; keep_vm={int(keep)}; cli_build_cache=/cache; cli_build_key=key; prepare_output_dir=/output; original_args=()\n"
                    result = subprocess.run(["bash", "-c", setup + lease + '\nprintf "%s\\n" "${lease_args[@]}"'], capture_output=True, text=True, check=True)
                    args = result.stdout.splitlines()
                    self.assertEqual(args.count("--cleanup-baseline"), 1)
                    self.assertEqual(args[args.index("--cleanup-baseline") + 1], "runtime-ready")
                    self.assertEqual("--keep-state" in args, keep)


if __name__ == "__main__":
    unittest.main()
