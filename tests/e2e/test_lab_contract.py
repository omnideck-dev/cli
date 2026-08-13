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
        self.assertNotIn("omnideck-cli-vm-e2e", script)
        self.assertNotIn("discarded-before", script)

    def test_windows_harness_uses_same_controller_contract(self) -> None:
        script = (ROOT / "tests/e2e/run-windows.sh").read_text(encoding="utf-8")
        self.assertIn("lease windows cli", script)
        self.assertIn('lab.sh" describe windows', script)
        self.assertIn('evidence-init', script)
        self.assertIn('evidence-finish', script)
        self.assertNotIn("omnideck-cli-vm-e2e", script)
        self.assertNotIn("windows-tpm.*", script)


if __name__ == "__main__":
    unittest.main()
