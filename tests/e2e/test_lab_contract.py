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
        self.assertIn('--cleanup-baseline clean', script)
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
        self.assertIn('--cleanup-baseline clean', script)
        self.assertNotIn("omnideck-cli-vm-e2e", script)
        self.assertNotIn("windows-tpm.*", script)

    def test_matrix_preflights_and_groups_lane_evidence(self) -> None:
        script = (ROOT / "tests/e2e/matrix.sh").read_text(encoding="utf-8")
        self.assertIn("preflight cli release-clean", script)
        self.assertIn("artifact-path cli matrix", script)
        self.assertIn('OMNIDECK_VM_E2E_OUTPUT_DIR="$lane_dir"', script)
        self.assertIn("lane-status.tsv", script)


if __name__ == "__main__":
    unittest.main()
