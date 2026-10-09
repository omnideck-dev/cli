"""Exercise signing failure gates without requiring Apple credentials."""
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class MacosSigningTests(unittest.TestCase):
    def test_signed_archive_preserves_bytes_and_normalizes_metadata(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            binary = root / 'omnideck'
            binary.write_bytes(b'Mach-O with signed content\0\1')
            outputs = [root / 'one.tar.gz', root / 'two.tar.gz']
            for output in outputs:
                subprocess.run(['python3', str(ROOT / 'scripts/macos/package-cli.py'),
                                str(binary), str(output), '1000'], check=True)
            self.assertEqual(outputs[0].read_bytes(), outputs[1].read_bytes())
            with tarfile.open(outputs[0]) as archive:
                self.assertEqual(archive.getnames(), ['omnideck'])
                item = archive.getmember('omnideck')
                self.assertEqual((item.uid, item.gid, item.mode, item.mtime), (0, 0, 0o755, 1000))
                self.assertEqual(archive.extractfile(item).read(), binary.read_bytes())

    def signing_run(self, status='Accepted', team='2FL6BUG8Q4'):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        tools = root / 'tools'
        tools.mkdir()
        trace = root / 'trace'
        binaries = {
            'uname': '#!/bin/bash\necho Darwin\n',
            'codesign': '''#!/bin/bash
printf '%s\\n' "$*" >> "$TRACE"
if [[ "$1" == --display ]]; then
  printf 'CodeDirectory v=20500 size=281 flags=0x10000(runtime)\\nAuthority=Developer ID Application: Test\\nTeamIdentifier=%s\\nTimestamp=Oct 8 2026\\n' "$TEAM"
fi
''',
            'ditto': '#!/bin/bash\ncp "$3" "$4"\n',
            'xcrun': '''#!/bin/bash
printf 'notarytool called\\n' >> "$TRACE"
printf '{"id":"test-submission","status":"%s"}\\n' "$STATUS"
''',
        }
        for name, text in binaries.items():
            file = tools / name
            file.write_text(text)
            file.chmod(0o755)
        binary = root / 'omnideck'
        binary.write_bytes(b'test signed Mach-O fixture')
        env = dict(os.environ, PATH=f'{tools}:'+os.environ['PATH'], TRACE=str(trace),
                   TEAM=team, STATUS=status, RUNNER_TEMP=str(root),
                   APPLE_SIGNING_IDENTITY='Developer ID Application: Test',
                   APPLE_TEAM_ID='2FL6BUG8Q4', APPLE_API_KEY='test',
                   APPLE_API_ISSUER='test', APPLE_API_KEY_PATH=str(root/'test.p8'))
        result = subprocess.run(['bash', str(ROOT/'scripts/macos/sign-and-notarize.sh'), str(binary)],
                                env=env, capture_output=True, text=True)
        return result, trace.read_text()

    def test_accepted_submission_checks_online_notarization(self):
        result, trace = self.signing_run()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('--options runtime --timestamp', trace)
        self.assertIn('--check-notarization', trace)

    def test_rejected_submission_blocks_packaging(self):
        result, trace = self.signing_run(status='Invalid')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('--check-notarization', trace)

    def test_pending_submission_is_not_treated_as_accepted(self):
        result, _ = self.signing_run(status='In Progress')
        self.assertNotEqual(result.returncode, 0)

    def test_wrong_team_never_reaches_notary_service(self):
        result, trace = self.signing_run(team='WRONGTEAM1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('notarytool called', trace)


if __name__ == '__main__':
    unittest.main()
