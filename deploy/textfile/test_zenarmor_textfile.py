#!/usr/bin/env python3
"""Exercise the optional command with real local logs and atomic publication."""
import datetime
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

COMMAND = Path(__file__).with_name('zenarmor-textfile.py')
PREFIX = 'opnsense_textfile_zenarmor_'


class ZenarmorTextfileTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir='/var/tmp')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.logs = self.root / 'logs'
        for name in ('active', 'archive'):
            (self.logs / name).mkdir(parents=True)
        self.output = self.root / 'output'
        self.output.mkdir()
        self.today = datetime.datetime.now().strftime('%Y-%m-%d')
        self.target = self.output / 'zenarmor.prom'

    def run_command(self, *arguments):
        result = subprocess.run([sys.executable, str(COMMAND), '--log-root', str(self.logs),
                                 '--output-dir', str(self.output), *arguments],
                                capture_output=True, text=True, timeout=15)
        self.assertTrue(self.target.exists(), result.stderr)
        return result, self.target.read_text()

    def test_calendar_counts_and_rotation(self):
        yesterday = (datetime.datetime.now() - datetime.timedelta(days=1)).strftime('%Y-%m-%d')
        active = self.logs / 'active' / 'main_current.log'
        active.write_text(f'{self.today} terminated with signal 11 terminated with signal 11\n'
                          f'{self.today} heartbeat for 30 heartbeat for 40\n'
                          f'{yesterday} terminated with signal 11\n')
        archive = self.logs / 'archive' / 'main_old.log'
        archive.write_text(f'{self.today} heartbeat for 20\n')
        os.link(active, self.logs / 'archive' / 'main_duplicate.log')
        result, text = self.run_command()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(PREFIX + 'crashes_today 1\n', text)
        self.assertIn(PREFIX + 'stalls_today 2\n', text)
        self.assertIn(PREFIX + 'collection_success 1\n', text)
        self.assertNotIn('{', text)
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o644)
        # Rename rotation retains exactly the same daily counts.
        active.rename(self.logs / 'archive' / 'main_rotated.log')
        active.write_text('ordinary log entry\n')
        result, rotated = self.run_command()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(PREFIX + 'crashes_today 1\n', rotated)
        self.assertIn(PREFIX + 'stalls_today 2\n', rotated)

    def test_missing_and_limits_do_not_report_zero(self):
        result, text = self.run_command()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(PREFIX + 'collection_success 0\n', text)
        self.assertNotIn('crashes_today', text)
        self.assertNotIn('day_start', text)
        active = self.logs / 'active' / 'main_current.log'
        active.write_text(f'{self.today} terminated with signal 11\n')
        result, text = self.run_command('--max-bytes', '1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('crashes_today', text)
        (self.logs / 'archive' / 'main_old.log').write_text('ordinary\n')
        result, text = self.run_command('--max-files', '1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('stalls_today', text)

    def test_nonregular_and_oversized_lines_fail_closed(self):
        active = self.logs / 'active' / 'main_current.log'
        os.mkfifo(active)
        result, text = self.run_command()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('crashes_today', text)
        active.unlink()
        active.write_bytes(b'x' * (1024 * 1024 + 1))
        result, text = self.run_command()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('crashes_today', text)
        active.unlink()
        active.symlink_to(self.root / 'nonexistent')
        result, text = self.run_command()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('crashes_today', text)

    def test_publication_symlinks_and_failure_preserve_old_file(self):
        (self.logs / 'active' / 'main_current.log').write_text('ordinary\n')
        victim = self.root / 'victim'
        victim.write_text('retain me')
        self.target.symlink_to(victim)
        result, text = self.run_command()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.target.is_symlink())
        self.assertEqual(victim.read_text(), 'retain me')
        held = self.root / 'held-output'
        self.output.rename(held)
        self.output.symlink_to(held, target_is_directory=True)
        result = subprocess.run([sys.executable, str(COMMAND), '--log-root', str(self.logs),
                                 '--output-dir', str(self.output)],
                                capture_output=True, text=True, timeout=15)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((held / 'zenarmor.prom').read_text(), text)

    def test_compressed_logs_and_directory_replacement_fail_closed(self):
        (self.logs / 'active' / 'main_current.log').write_text('ordinary\n')
        (self.logs / 'archive' / 'main_old.log.gz').write_bytes(b'compressed')
        result, text = self.run_command()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('crashes_today', text)
        (self.logs / 'archive' / 'main_old.log.gz').unlink()
        archive = self.logs / 'archive'
        archive.rename(self.logs / 'held-archive')
        archive.symlink_to(self.logs / 'held-archive', target_is_directory=True)
        result, text = self.run_command()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('stalls_today', text)

    def test_midnight_is_rejected_and_next_day_resets(self):
        spec = importlib.util.spec_from_file_location('zenarmor_textfile', COMMAND)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        today = datetime.datetime(2026, 10, 15, 23, 59, 59)
        tomorrow = datetime.datetime(2026, 10, 16, 0, 0, 1)
        (self.logs / 'active' / 'main_current.log').write_text(
            '2026-10-15 terminated with signal 11\n2026-10-16 heartbeat for 30\n')
        clocks = iter([today, tomorrow])
        with self.assertRaises(module.CollectionError):
            module.collect(str(self.logs), 64 * 1024 * 1024, 128, now=lambda: next(clocks))
        counts = module.collect(str(self.logs), 64 * 1024 * 1024, 128, now=lambda: tomorrow)
        self.assertEqual(counts[:2], (0, 1))
        # A same-day rename/replacement during collection cannot publish a partial count.
        calls = 0

        def rotate_during_scan():
            nonlocal calls
            calls += 1
            if calls == 2:
                active = self.logs / 'active' / 'main_current.log'
                active.rename(self.logs / 'archive' / 'main_rotated.log')
                active.write_text('2026-10-16 terminated with signal 11\n')
            return tomorrow

        with self.assertRaises(module.CollectionError):
            module.collect(str(self.logs), 64 * 1024 * 1024, 128,
                           now=rotate_during_scan)


if __name__ == '__main__':
    unittest.main()
