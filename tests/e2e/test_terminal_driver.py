import importlib.util
import argparse
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("terminal_driver.py")
SPEC = importlib.util.spec_from_file_location("omnideck_terminal_driver", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
DRIVER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DRIVER)


class StripTerminalControlsTest(unittest.TestCase):
    def test_strips_cursor_color_and_alt_screen_controls(self) -> None:
        raw = b"\x1b[?1049h\x1b[2J\x1b[31mWelcome to omnideck\x1b[0m\r\n\x1b[?1049l"
        self.assertEqual(DRIVER.strip_terminal_controls(raw), "Welcome to omnideck\n")

    def test_strips_osc_title_but_keeps_unicode(self) -> None:
        raw = "\x1b]0;omnideck\x07✓ Decks › omnideck".encode()
        self.assertEqual(DRIVER.strip_terminal_controls(raw), "✓ Decks › omnideck")

    def test_phrase_matching_ignores_layout_spacing(self) -> None:
        screen = "enter  set up omnideck\nq  cancel"
        self.assertTrue(DRIVER.contains_rendered_phrase(screen, "enter set up omnideck"))
        self.assertFalse(DRIVER.contains_rendered_phrase(screen, "enter remove omnideck"))


class ScenarioCommandTest(unittest.TestCase):
    def test_custom_command_is_a_json_string_array(self) -> None:
        args = argparse.Namespace(command_json='["ssh", "-tt", "tester@127.0.0.1", "omnideck.exe tui"]')
        self.assertEqual(
            DRIVER.scenario_command(args, ["fallback"]),
            ["ssh", "-tt", "tester@127.0.0.1", "omnideck.exe tui"],
        )

    def test_custom_command_rejects_shell_text(self) -> None:
        args = argparse.Namespace(command_json='"ssh tester@127.0.0.1"')
        with self.assertRaisesRegex(ValueError, "JSON array of strings"):
            DRIVER.scenario_command(args, ["fallback"])


if __name__ == "__main__":
    unittest.main()
