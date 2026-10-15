#!/usr/bin/env python3
"""Exercise the shipped root-cron command through mocked FreeBSD process edges."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

BASE = Path(__file__).resolve().parent


class NICTextfileTest(unittest.TestCase):
    def test_publication_and_failed_read(self):
        with tempfile.TemporaryDirectory(dir="/var/tmp") as directory:
            root = Path(directory)
            output = root / 'output'
            output.mkdir()
            commands = root / 'bin'
            commands.mkdir()
            for name, fixture in [('sysctl', 'sysctl.txt'), ('vmstat', 'vmstat.txt')]:
                command = commands / name
                command.write_text('#!/bin/sh\n[ "${FAIL_READ:-}" != "1" ] || exit 1\ncat "' + str(BASE / 'fixtures' / fixture) + '"\n')
                command.chmod(0o755)
            # Substitute only the interpreter process edge on non-FreeBSD test hosts.
            runtime = root / 'nic-textfile.sh'
            runtime.write_text((BASE / 'nic-textfile.sh').read_text().replace(
                '/usr/local/bin/python3', sys.executable).replace(
                    'PATH=/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/sbin:/usr/local/bin',
                    'PATH=' + str(commands) + ':' + os.environ['PATH']))
            (root / 'nic-textfile.awk').write_bytes((BASE / 'nic-textfile.awk').read_bytes())
            env = dict(os.environ, PATH=str(commands) + ':' + os.environ['PATH'])
            subprocess.run(['sh', str(runtime), str(output)], env=env, check=True)
            target = output / 'nic.prom'
            text = target.read_text()
            self.assertIn('opnsense_textfile_nic_receive_discards_total{device="ixl2",driver="ixl"} 101777', text)
            self.assertIn('opnsense_textfile_nic_rx_queue_packets_total{device="ixl2",driver="ixl",queue="00"} 727875102', text)
            self.assertIn('opnsense_textfile_nic_rx_queue_interrupts_total{device="ixl2",driver="ixl",queue="00"} 402177179', text)
            self.assertIn('opnsense_textfile_interrupts_total{interrupt="irq262: ixl2:que 0"} 402177179', text)
            self.assertIn('opnsense_textfile_netmap_info{setting="admode",value="0"} 1', text)
            self.assertIn('opnsense_textfile_netmap_iflib_rx_miss_total 121', text)
            self.assertNotIn('value="121"', text)
            self.assertNotIn('unknown', text)
            self.assertNotIn('rxq999', text)
            self.assertNotIn('999999999999', text)
            self.assertIn('opnsense_textfile_last_success_timestamp_seconds ', text)
            failed = subprocess.run(['sh', str(runtime), str(output)], env=dict(env, FAIL_READ='1'))
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(target.read_text(), text)
            self.assertEqual(sorted(p.name for p in output.iterdir()), ['nic.prom'])
            target.unlink()
            victim = root / 'victim'
            victim.write_text('retain me')
            target.symlink_to(victim)
            subprocess.run(['sh', str(runtime), str(output)], env=env, check=True)
            self.assertFalse(target.is_symlink())
            self.assertIn('opnsense_textfile_nic_receive_discards_total', target.read_text())
            self.assertEqual(victim.read_text(), 'retain me')
            target.unlink()
            target.mkdir()
            rejected = subprocess.run(['sh', str(runtime), str(output)], env=env)
            self.assertNotEqual(rejected.returncode, 0)
            self.assertEqual(list(target.iterdir()), [])
            target.rmdir()
            target.write_text(text)
            # Reject a collector path replaced with a symlink to a different directory.
            held = root / 'held-output'
            output.rename(held)
            other = root / 'other-output'
            other.mkdir()
            output.symlink_to(other, target_is_directory=True)
            parent_rejected = subprocess.run(['sh', str(runtime), str(output)], env=env)
            self.assertNotEqual(parent_rejected.returncode, 0)
            self.assertEqual(list(other.iterdir()), [])
            self.assertEqual((held / 'nic.prom').read_text(), text)
            output.unlink()
            held.rename(output)
            # A successful command with corrupt selected data must also preserve the snapshot.
            (commands / 'sysctl').write_text('#!/bin/sh\nprintf "dev.ixl.2.pf.rx_discards: broken\\n"\n')
            corrupt = subprocess.run(['sh', str(runtime), str(output)], env=env)
            self.assertNotEqual(corrupt.returncode, 0)
            self.assertEqual(target.read_text(), text)
            # Discovery cannot create an unlimited device label set.
            (commands / 'sysctl').write_text('#!/bin/sh\n' + ''.join(
                f'echo "dev.ixl.{index}.pf.rx_discards: 0"\n' for index in range(65)))
            overflow = subprocess.run(['sh', str(runtime), str(output)], env=env)
            self.assertNotEqual(overflow.returncode, 0)
            self.assertEqual(target.read_text(), text)


if __name__ == '__main__':
    unittest.main()
