#!/usr/bin/env python3
"""Black-box terminal driver for the CLI VM E2E suite.

The driver deliberately executes the compiled CLI in a real PTY. Assertions
are phrased in user-visible language instead of implementation state so a
refactor may reorganize internals while the terminal contract remains stable.
"""

from __future__ import annotations

import argparse
import errno
import fcntl
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time
from pathlib import Path
from typing import Iterable, Sequence


CSI_RE = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]")
OSC_RE = re.compile(rb"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")
CONTROL_RE = re.compile(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]")


def strip_terminal_controls(value: bytes) -> str:
    """Return searchable terminal text while retaining every rendered phrase."""

    value = OSC_RE.sub(b"", value)
    value = CSI_RE.sub(b"", value)
    text = value.decode("utf-8", errors="replace").replace("\r", "")
    return CONTROL_RE.sub("", text)


def contains_rendered_phrase(screen: str, phrase: str) -> bool:
    """Match semantic text without coupling assertions to TUI column spacing."""

    if phrase in screen:
        return True
    return " ".join(phrase.split()) in " ".join(screen.split())


class TerminalSession:
    def __init__(
        self,
        command: Sequence[str],
        *,
        env: dict[str, str],
        artifact_dir: Path,
        name: str,
        width: int = 120,
        height: int = 40,
    ) -> None:
        self.command = list(command)
        self.artifact_dir = artifact_dir
        self.name = name
        self.raw = bytearray()
        self.events: list[dict[str, object]] = []
        self.started = time.monotonic()

        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        self.master = master
        self.process = subprocess.Popen(
            self.command,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            env=env,
            close_fds=True,
            start_new_session=True,
        )
        os.close(slave)
        self.record("spawn", command=self.command, width=width, height=height)

    def record(self, event: str, **fields: object) -> None:
        entry: dict[str, object] = {
            "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "event": event,
        }
        entry.update(fields)
        self.events.append(entry)

    def _read_once(self, timeout: float) -> bool:
        readable, _, _ = select.select([self.master], [], [], timeout)
        if not readable:
            return False
        try:
            chunk = os.read(self.master, 65536)
        except OSError as exc:
            if exc.errno == errno.EIO and self.process.poll() is not None:
                return False
            raise
        if not chunk:
            return False
        self.raw.extend(chunk)
        return True

    def mark(self) -> int:
        return len(self.raw)

    def expect_all(
        self,
        phrases: Iterable[str],
        *,
        timeout: float,
        since: int = 0,
        checkpoint: str,
        fail_phrases: Iterable[str] = (),
    ) -> None:
        wanted = list(phrases)
        fatal = list(fail_phrases)
        deadline = time.monotonic() + timeout
        while True:
            searchable = strip_terminal_controls(bytes(self.raw[max(0, since - 32) :]))
            observed_failure = [phrase for phrase in fatal if contains_rendered_phrase(searchable, phrase)]
            if observed_failure:
                raise AssertionError(
                    f"{self.name} reached an error screen before {checkpoint}: {observed_failure!r}"
                )
            missing = [phrase for phrase in wanted if not contains_rendered_phrase(searchable, phrase)]
            if not missing:
                self.record("checkpoint", name=checkpoint, phrases=wanted)
                return
            if self.process.poll() is not None:
                self._read_once(0)
                raise AssertionError(
                    f"{self.name} exited {self.process.returncode} before {checkpoint}; "
                    f"missing {missing!r}"
                )
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise AssertionError(f"timed out at {checkpoint}; missing {missing!r}")
            self._read_once(min(0.25, remaining))

    def send(self, value: bytes, *, label: str) -> int:
        mark = self.mark()
        os.write(self.master, value)
        self.record("key", key=label)
        return mark

    def wait(self, timeout: float = 15) -> int:
        deadline = time.monotonic() + timeout
        while self.process.poll() is None and time.monotonic() < deadline:
            self._read_once(0.1)
        if self.process.poll() is None:
            raise AssertionError(f"{self.name} did not exit within {timeout} seconds")
        while self._read_once(0):
            pass
        self.record("exit", returnCode=self.process.returncode)
        return int(self.process.returncode)

    def close(self) -> None:
        if self.process.poll() is None:
            try:
                os.killpg(self.process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(self.process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                self.process.wait(timeout=5)
        while self._read_once(0):
            pass
        os.close(self.master)
        self.artifact_dir.mkdir(parents=True, exist_ok=True)
        (self.artifact_dir / f"{self.name}.raw").write_bytes(self.raw)
        (self.artifact_dir / f"{self.name}.txt").write_text(
            strip_terminal_controls(bytes(self.raw)), encoding="utf-8"
        )
        with (self.artifact_dir / f"{self.name}-events.ndjson").open("w", encoding="utf-8") as stream:
            for event in self.events:
                stream.write(json.dumps(event, sort_keys=True) + "\n")

    def __enter__(self) -> "TerminalSession":
        return self

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self.close()


ENTER = b"\r"
ESCAPE = b"\x1b"
DOWN = b"\x1b[B"


def base_environment(args: argparse.Namespace) -> dict[str, str]:
    env = os.environ.copy()
    env.update(
        {
            "TERM": "xterm-256color",
            "NO_COLOR": "",
            "OMNIDECK_CONFIG_DIR": str(Path(args.config_dir).resolve()),
            "CONTAINERS_REGISTRIES_CONF": str(Path(args.registries_conf).resolve()),
        }
    )
    return env


def scenario_command(args: argparse.Namespace, fallback: Sequence[str]) -> list[str]:
    if not args.command_json:
        return list(fallback)
    command = json.loads(args.command_json)
    if not isinstance(command, list) or not command or not all(isinstance(value, str) for value in command):
        raise ValueError("--command-json must be a non-empty JSON array of strings")
    return command


def run_hook(args: argparse.Namespace, terminal: TerminalSession, label: str) -> None:
    if not args.hook_command_json:
        raise AssertionError(f"{label} requires --hook-command-json")
    command = json.loads(args.hook_command_json)
    if not isinstance(command, list) or not command or not all(isinstance(value, str) for value in command):
        raise ValueError("--hook-command-json must be a non-empty JSON array of strings")
    terminal.record("hook", name=label, command=command)
    subprocess.run(command, check=True)


def install_scenario(args: argparse.Namespace) -> None:
    env = base_environment(args)
    command = scenario_command(args, [args.binary, "install", "--image", args.fixture_image])
    with TerminalSession(command, env=env, artifact_dir=args.artifact_dir, name="install") as terminal:
        terminal.expect_all(
            ["Welcome to omnideck", "Press Enter to set up omnideck.", "enter set up omnideck"],
            timeout=30,
            checkpoint="welcome",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            [
                "Preparing your environment",
                "Setting omnideck up on this computer. This usually takes a few minutes.",
                "Getting your computer ready…",
                "Computer setup",
                "Application files",
                "Final checks",
            ],
            timeout=45,
            since=mark,
            checkpoint="quick-check",
        )
        terminal.expect_all(
            [
                "Waiting for your permission",
                "Your computer will ask you to approve installing Podman — the software omnideck uses to run in an isolated space. omnideck never sees or stores your password.",
            ],
            timeout=60,
            since=mark,
            checkpoint="runtime-permission",
        )
        terminal.expect_all(
            [
                "omnideck is ready",
                "Everything is prepared. Open omnideck whenever you’re ready.",
                "Open Omnideck in your browser:",
                "http://localhost:2337",
                "Your files and settings will be kept when Omnideck updates.",
                "Press any key to return to the dashboard.",
            ],
            timeout=args.install_timeout,
            since=mark,
            checkpoint="ready",
            fail_phrases=("The download didn’t finish", "Setup couldn’t finish"),
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Decks", "omnideck", ":2337", "running", "Open UI", "Logs", "Update", "Stop"],
            timeout=45,
            since=mark,
            checkpoint="first-dashboard",
        )
        terminal.send(b"q", label="q")
        if terminal.wait() != 0:
            raise AssertionError("install journey did not exit successfully")


def manage_scenario(args: argparse.Namespace) -> None:
    env = base_environment(args)
    command = scenario_command(args, [args.binary, "tui"])
    log_match_counter = f"{args.expected_log_count} of {args.expected_log_count}"
    with TerminalSession(command, env=env, artifact_dir=args.artifact_dir, name="manage") as terminal:
        terminal.expect_all(
            ["Decks", "omnideck", ":2337", "running", "Open UI", "Logs", "Update", "Stop"],
            timeout=45,
            checkpoint="dashboard",
        )

        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["METADATA", "RESOURCES", "LOGS · TAIL", "Remove instance", "omnideck-hardware-fixture-started"],
            timeout=30,
            since=mark,
            checkpoint="expanded-card",
        )

        mark = terminal.send(b"l", label="l")
        terminal.expect_all(
            ["Decks › omnideck › Logs", "omnideck stdout + stderr", "omnideck-hardware-fixture-started", "/ search"],
            timeout=30,
            since=mark,
            checkpoint="logs",
        )
        mark = terminal.send(b"/", label="/")
        terminal.expect_all(
            ["type to filter…", "type filter", "enter done", "esc clear"],
            timeout=15,
            since=mark,
            checkpoint="log-filter-input",
        )
        mark = terminal.send(b"fixture", label="fixture")
        terminal.expect_all(
            ["fixture", log_match_counter],
            timeout=15,
            since=mark,
            checkpoint="log-filter",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["fixture", log_match_counter, "esc clear filter", "/ edit filter"],
            timeout=15,
            since=mark,
            checkpoint="log-filter-applied",
        )
        mark = terminal.send(ESCAPE, label="esc")
        terminal.expect_all(
            ["omnideck stdout + stderr", "esc back", "/ search"],
            timeout=15,
            since=mark,
            checkpoint="log-filter-cleared",
        )
        mark = terminal.send(ESCAPE, label="esc")
        terminal.expect_all(["Decks", "omnideck", "running"], timeout=20, since=mark, checkpoint="logs-back")

        mark = terminal.send(b"c", label="c")
        terminal.expect_all(
            ["Decks › omnideck › Settings", "File storage", "App storage", "Memory limit", "Browser port", "Container image"],
            timeout=20,
            since=mark,
            checkpoint="settings",
        )
        mark = terminal.send(ESCAPE, label="esc")
        terminal.expect_all(["Decks", "omnideck", "running"], timeout=20, since=mark, checkpoint="settings-back")

        mark = terminal.send(b"u", label="u")
        terminal.expect_all(
            ["Decks › omnideck › Update", "Update omnideck", "files and agent data are kept", "enter update"],
            timeout=20,
            since=mark,
            checkpoint="update-review",
        )
        mark = terminal.send(ESCAPE, label="esc")
        terminal.expect_all(["Decks", "omnideck", "running"], timeout=20, since=mark, checkpoint="update-canceled")

        mark = terminal.send(b"s", label="s")
        terminal.expect_all(
            ["Stopped omnideck", "Start"], timeout=45, since=mark, checkpoint="stopped"
        )
        mark = terminal.send(b"d", label="d")
        terminal.expect_all(
            [
                "Doctor",
                "1 problem needs attention",
                "Omnideck instance — omnideck is stopped",
                "Press Enter to start omnideck.",
            ],
            timeout=45,
            since=mark,
            checkpoint="doctor-stopped",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Everything required is working", "Omnideck instance — omnideck is running", "Browser — http://localhost:2337 is responding"],
            timeout=60,
            since=mark,
            checkpoint="doctor-recovered",
        )
        mark = terminal.send(ESCAPE, label="esc")
        terminal.expect_all(["Decks", "omnideck", "running"], timeout=20, since=mark, checkpoint="doctor-back")

        mark = terminal.send(b"n", label="n")
        terminal.expect_all(
            ["Setup · Settings", "Recommended settings are ready", "omnideck2", "http://localhost:2338"],
            timeout=45,
            since=mark,
            checkpoint="additional-instance-defaults",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Setup · Review", "Ready to set up Omnideck", "Name", "omnideck2", "Press Enter to start setup"],
            timeout=20,
            since=mark,
            checkpoint="additional-instance-review",
        )
        mark = terminal.send(b"q", label="q")
        terminal.expect_all(["Decks", "omnideck", "running"], timeout=20, since=mark, checkpoint="additional-instance-canceled")

        mark = terminal.send(b"x", label="x")
        terminal.expect_all(
            ["Remove omnideck", "Keep saved data — Recommended", "Permanently delete saved data"],
            timeout=20,
            since=mark,
            checkpoint="removal-data-choice",
        )
        terminal.send(DOWN, label="down")
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Protect your data before deleting it", "Create a backup — Recommended", "Delete without a backup"],
            timeout=20,
            since=mark,
            checkpoint="removal-backup-choice",
        )
        terminal.send(DOWN, label="down")
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Confirm permanent data deletion", "Type omnideck below to confirm", "No backup will be created"],
            timeout=20,
            since=mark,
            checkpoint="removal-confirmation",
        )
        terminal.send(b"omnideck", label="omnideck")
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["omnideck was removed", "saved data was permanently deleted", "Press any key to return to Decks"],
            timeout=60,
            since=mark,
            checkpoint="removal-complete",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Decks", "No Omnideck instances are set up yet", "Press n to set one up"],
            timeout=30,
            since=mark,
            checkpoint="empty-dashboard",
        )
        terminal.send(b"q", label="q")
        if terminal.wait() != 0:
            raise AssertionError("management journey did not exit successfully")


def windows_bootstrap_scenario(args: argparse.Namespace) -> None:
    env = base_environment(args)
    command = scenario_command(args, [])
    with TerminalSession(command, env=env, artifact_dir=args.artifact_dir, name="windows-bootstrap") as terminal:
        terminal.expect_all(
            ["Welcome to omnideck", "Press Enter to set up omnideck.", "enter set up omnideck"],
            timeout=30,
            checkpoint="welcome",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            [
                "Preparing your environment",
                "Setting omnideck up on this computer. This usually takes a few minutes.",
                "Computer setup",
                "Secure space",
                "Application files",
                "Final checks",
            ],
            timeout=60,
            since=mark,
            checkpoint="quick-check",
        )
        terminal.expect_all(
            [
                "Waiting for your permission",
                "Your computer will ask you to approve turning on Windows Subsystem for Linux, which omnideck needs to run in an isolated space. omnideck never sees or stores your password.",
            ],
            timeout=60,
            since=mark,
            checkpoint="windows-permission",
        )
        run_hook(args, terminal, "approve-windows-security")
        terminal.expect_all(
            [
                "Restart needed",
                "Windows must restart to finish enabling required features. Save any open work, then restart now or later. If you restart now, omnideck reopens after you sign in and continues setup.",
                "Press Enter to restart now",
                "Press l to restart later",
            ],
            timeout=600,
            since=mark,
            checkpoint="restart-needed",
        )
        terminal.send(b"l", label="l")
        if terminal.wait(30) != 0:
            raise AssertionError("Windows prerequisite journey did not exit successfully")


def windows_install_scenario(args: argparse.Namespace) -> None:
    env = base_environment(args)
    command = scenario_command(args, [])
    with TerminalSession(command, env=env, artifact_dir=args.artifact_dir, name="windows-install") as terminal:
        terminal.expect_all(
            ["Welcome to omnideck", "Press Enter to set up omnideck.", "enter set up omnideck"],
            timeout=30,
            checkpoint="welcome",
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            [
                "Preparing your environment",
                "Setting omnideck up on this computer. This usually takes a few minutes.",
                "Computer setup",
                "Secure space",
                "Application files",
                "Final checks",
            ],
            timeout=60,
            since=mark,
            checkpoint="quick-check",
        )
        terminal.expect_all(["Downloading Podman…"], timeout=900, since=mark, checkpoint="download-podman")
        terminal.expect_all(["Installing Podman…"], timeout=1200, since=mark, checkpoint="install-podman")
        terminal.expect_all(
            ["Preparing a secure space to run in…", "Secure space"],
            timeout=600,
            since=mark,
            checkpoint="secure-space",
        )
        run_hook(args, terminal, "install-fixture-registry-ca")
        terminal.expect_all(
            [
                "omnideck is ready",
                "Everything is prepared. Open omnideck whenever you’re ready.",
                "Open Omnideck in your browser:",
                "http://localhost:2337",
                "Your files and settings will be kept when Omnideck updates.",
                "Press any key to return to the dashboard.",
            ],
            timeout=args.install_timeout,
            since=mark,
            checkpoint="ready",
            fail_phrases=("The download didn’t finish", "Setup couldn’t finish"),
        )
        mark = terminal.send(ENTER, label="enter")
        terminal.expect_all(
            ["Decks managed by this host", "omnideck", ":2337", "Open UI", "Logs", "Update", "Stop"],
            timeout=60,
            since=mark,
            checkpoint="first-dashboard",
        )
        terminal.send(b"q", label="q")
        if terminal.wait(30) != 0:
            raise AssertionError("Windows install journey did not exit successfully")


def parse_args(argv: Sequence[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("scenario", choices=("install", "manage", "windows-bootstrap", "windows-install"))
    parser.add_argument("--binary", required=True)
    parser.add_argument("--config-dir", required=True)
    parser.add_argument("--registries-conf", required=True)
    parser.add_argument("--fixture-image", required=True)
    parser.add_argument("--artifact-dir", required=True, type=Path)
    parser.add_argument("--install-timeout", type=float, default=1200)
    parser.add_argument("--expected-log-count", type=int, default=1)
    parser.add_argument("--command-json")
    parser.add_argument("--hook-command-json")
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    args.artifact_dir.mkdir(parents=True, exist_ok=True)
    try:
        if args.scenario == "install":
            install_scenario(args)
        elif args.scenario == "manage":
            manage_scenario(args)
        elif args.scenario == "windows-bootstrap":
            windows_bootstrap_scenario(args)
        else:
            windows_install_scenario(args)
    except Exception as exc:  # noqa: BLE001 - top-level evidence boundary
        print(f"E2E FAILURE: {exc}", file=sys.stderr)
        return 1
    print(f"PASS: {args.scenario} terminal journey")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
