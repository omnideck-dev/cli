from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class LabHarnessContractTests(unittest.TestCase):
    def test_linux_harness_uses_controller_contract(self) -> None:
        script = (ROOT / "tests/e2e/run.sh").read_text(encoding="utf-8")
        self.assertIn('lease "${vm}" cli', script)
        self.assertIn('lab.sh" describe', script)
        self.assertIn('evidence-init', script)
        self.assertIn('evidence-finish', script)
        self.assertIn('prepare_cli_binaries linux', script)
        self.assertIn('artifact-path cli e2e', script)
        self.assertIn('--cleanup-baseline "$baseline"', script)
        self.assertNotIn("omnideck-cli-vm-e2e", script)
        self.assertNotIn("discarded-before", script)

    def test_windows_harness_uses_same_controller_contract(self) -> None:
        script = (ROOT / "tests/e2e/run-windows.sh").read_text(encoding="utf-8")
        self.assertIn("lease windows cli", script)
        self.assertIn('lab.sh" describe windows', script)
        self.assertIn('evidence-init', script)
        self.assertIn('evidence-finish', script)
        self.assertIn('prepare_cli_binaries windows', script)
        self.assertIn('artifact-path cli e2e', script)
        self.assertIn('--cleanup-baseline "$baseline"', script)
        self.assertNotIn("omnideck-cli-vm-e2e", script)
        self.assertNotIn("windows-tpm.*", script)

    def test_matrix_preflights_and_groups_lane_evidence(self) -> None:
        script = (ROOT / "tests/e2e/matrix.sh").read_text(encoding="utf-8")
        self.assertIn('preflight cli "$profile"', script)
        self.assertIn("artifact-path cli matrix", script)
        self.assertIn("interrupt_matrix 130 INT", script)
        self.assertIn('wait "$active_lane_pid"', script)
        self.assertIn('OMNIDECK_VM_E2E_OUTPUT_DIR="$lane_dir"', script)
        self.assertIn("lane-status.tsv", script)

    def test_macos_harness_uses_physical_host_contract_and_ready_runtime(self) -> None:
        script = (ROOT / "tests/e2e/run-macos-lab.sh").read_text(encoding="utf-8")
        guest = (ROOT / "tests/e2e/macos_guest.sh").read_text(encoding="utf-8")
        self.assertIn('lease "$target" cli-e2e', script)
        self.assertIn("artifact-path cli macos-e2e", script)
        self.assertIn("GOOS=darwin", script)
        self.assertIn("GOARCH=arm64", script)
        self.assertIn("--cleanup-baseline runtime-ready", script)
        self.assertIn("podmanSetup=excluded-ready-runtime", script)
        self.assertIn("terminal_driver.py\" macos-install", guest)
        self.assertIn("terminal_driver.py\" manage", guest)
        self.assertIn("ownership_marker", guest)
        self.assertNotIn("podman machine init", guest)
        self.assertNotIn("podman machine start", guest)

    def test_legacy_manual_helper_delegates_to_canonical_lane(self) -> None:
        script = (ROOT / "tests/manual/run-local-hardware.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn('exec "${repo_root}/tests/e2e/run.sh"', script)
        self.assertNotIn("default_ssh_port", script)
        self.assertNotIn("artifacts/cli-hardware", script)
        self.assertNotIn("docker build", script)


if __name__ == "__main__":
    unittest.main()
