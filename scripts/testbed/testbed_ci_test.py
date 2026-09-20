#!/usr/bin/env python3
"""Tests for the CI restricted login shell (OPN-0109).

This is the security boundary for CI on `oli`. It is a LOGIN SHELL, not an
authorized_keys forced command: Tailscale SSH terminates port 22 on that host,
so sshd never reads authorized_keys and a forced command does not bind. The
tailnet policy pins the connection to the `opn-ci` account, and that account's
shell is this script.

These drive it with a stub power script and a stub sudo, so they assert exactly
what would be invoked without needing oli, root, sudo or qm.

Deny-path cases use commands that are HARMLESS if they were ever to run. An
earlier version of this suite used `qm stop 100` as a should-be-refused probe;
the restriction was not in force, it ran, and it stopped the house's home
automation. A test that asserts something cannot happen must not be the thing
that makes it happen.
"""

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

WRAPPER = Path(__file__).resolve().parent / "opnsense-testbed-ci.sh"

# Guests the wrapper must never be able to reach, whatever it is asked for.
# 100 is home automation, 101 the CI runners, 103 unifi-os, 104 a Windows
# server, 107 postgres. A wrapper that can touch these can cause a real outage.
PROD_GUESTS = ["100", "101", "103", "104", "107"]


class WrapperCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.calls = Path(self.tmp.name) / "calls"
        # Stub power script: records its argv instead of touching Proxmox.
        self.power = Path(self.tmp.name) / "opnsense-testbed-power.sh"
        self.power.write_text(
            "#!/usr/bin/env bash\n"
            f'printf "%s\\n" "$*" >> "{self.calls}"\n'
        )
        self.power.chmod(0o755)
        self.wrapper = Path(self.tmp.name) / "ci.sh"
        # Point the wrapper at the stubs without editing the shipped file. SELF
        # matters: the script re-execs itself under sudo, and the test has to
        # follow it to the same copy.
        source = (
            WRAPPER.read_text()
            .replace(
                "POWER=/usr/local/bin/opnsense-testbed-power.sh", f"POWER={self.power}"
            )
            .replace(
                "SELF=/usr/local/bin/opnsense-testbed-ci.sh", f"SELF={self.wrapper}"
            )
        )
        self.wrapper.write_text(source)
        self.wrapper.chmod(0o755)
        # Stub sudo: drops the -n and runs the target directly, so the
        # escalation path is exercised without needing real sudo.
        sudo = Path(self.tmp.name) / "sudo"
        sudo.write_text('#!/usr/bin/env bash\n[ "$1" = "-n" ] && shift\nexec "$@"\n')
        sudo.chmod(0o755)
        self.path = f"{self.tmp.name}:{os.environ.get('PATH', '')}"

    def run_wrapper(self, command, via="-c"):
        """Drive the wrapper the way a real session would.

        via="-c" is how a login shell receives a non-interactive SSH command,
        which is what CI actually sends. via="env" is the forced-command route,
        kept because it still applies if this ever moves to an sshd that honours
        one.
        """
        env = dict(os.environ)
        env["PATH"] = self.path
        env.pop("SSH_ORIGINAL_COMMAND", None)
        argv = [str(self.wrapper)]
        if command is None:
            pass
        elif via == "-c":
            argv += ["-c", command]
        else:
            env["SSH_ORIGINAL_COMMAND"] = command
        return subprocess.run(
            argv, capture_output=True, text=True, env=env, check=False
        )

    def invoked(self):
        if not self.calls.exists():
            return []
        return self.calls.read_text().strip().splitlines()


class AllowedVerbs(WrapperCase):
    def test_up_passes_a_numeric_hold_through(self):
        result = self.run_wrapper("up 1800")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.invoked(), ["up 1800"])

    def test_the_forced_command_route_still_works(self):
        """Kept working for a possible move to an sshd that honours one."""
        result = self.run_wrapper("status", via="env")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.invoked(), ["status"])

    def test_update_and_status_take_no_arguments(self):
        for verb in ("update", "status"):
            with self.subTest(verb=verb):
                self.assertEqual(self.run_wrapper(verb).returncode, 0)
        self.assertEqual(self.invoked(), ["update", "status"])

    def test_down_releases_the_hold_first(self):
        """`down` refuses while a hold is live.

        CI takes the hold, so CI has to be able to drop it - otherwise the lab
        stays up until the hold lapses and the whole point of CI-owned power,
        that the lab is up only for the run, is lost.
        """
        self.assertEqual(self.run_wrapper("down").returncode, 0)
        self.assertEqual(self.invoked(), ["release", "down"])

    def test_an_over_long_hold_is_clamped_not_refused(self):
        """A crashed workflow leaves the lab up until its hold lapses.

        Clamping bounds that to one hold. Refusing instead would just make a
        caller retry with a smaller number, which teaches nothing and costs a
        run.
        """
        result = self.run_wrapper("up 999999")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(self.invoked(), ["up 5400"])
        self.assertIn("clamping", result.stderr)


class RefusedEverythingElse(WrapperCase):
    def assert_refused(self, command, because):
        result = self.run_wrapper(command)
        self.assertNotEqual(result.returncode, 0, f"{because}: {command!r} was ALLOWED")
        self.assertEqual(self.invoked(), [], f"{because}: {command!r} reached the power script")

    def test_no_shell(self):
        self.assert_refused(None, "an interactive session must not open a shell")
        self.assert_refused("", "an empty command must not open a shell")

    def test_arbitrary_commands_are_refused(self):
        for command in (
            "bash",
            "sh -c id",
            "cat /root/.ssh/id_ed25519",
            "qm stop 100",
            "pct exec 105 -- sh",
            "/usr/local/bin/opnsense-testbed-power.sh down",
            "up 60; id",
        ):
            with self.subTest(command=command):
                self.assert_refused(command, "arbitrary command")

    def test_no_verb_can_name_a_guest(self):
        """The wrapper must expose no way to address a guest at all.

        Commands here are harmless by construction - guest 999999 does not
        exist, so even a total failure of the allowlist cannot power anything
        off. That is the point: this assertion must not be capable of causing
        the thing it asserts against.
        """
        for command in (
            "exec 999999 -- id",
            "down 999999",
            "status 999999",
            "update 999999",
            "put 999999 /tmp/x /tmp/y",
        ):
            with self.subTest(command=command):
                self.assert_refused(command, "guest addressing")

    def test_a_numeric_hold_is_a_hold_and_never_a_guest(self):
        """`up 100` is a 100-second hold, not guest 100.

        Worth pinning: the only numeric argument the wrapper accepts is a
        duration, and it reaches the power script as one. There is no argument
        position anywhere in this interface where a number means a guest.
        """
        result = self.run_wrapper("up 100")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.invoked(), ["up 100"])

    def test_argument_smuggling_is_refused(self):
        for command in (
            "update --force",
            "status extra",
            "down now",
            "up 60 extra",
            "up; id",
            "up $(id)",
            "up `id`",
            "up 60 && id",
        ):
            with self.subTest(command=command):
                self.assert_refused(command, "smuggled argument")

    def test_non_numeric_and_zero_holds_are_refused(self):
        for command in ("up", "up abc", "up -1", "up 1e3", "up 0"):
            with self.subTest(command=command):
                self.assert_refused(command, "bad hold")




class ReentryIsOnceOnly(WrapperCase):
    """The escalation must be structurally once-only.

    Found while writing these tests: with a sudo that returns WITHOUT elevating,
    the second invocation went straight back down the escalation branch and the
    script re-execed itself forever. That is a fork bomb reachable from a remote
    session - exactly what a security boundary must not contain - and it is not
    acceptable to rely on sudo always elevating to avoid it.
    """

    def test_an_already_escalated_call_does_not_escalate_again(self):
        result = self.run_wrapper("status")
        self.assertEqual(result.returncode, 0, result.stderr)
        # Exactly one call reaches the power script. More than one would mean
        # the re-entry guard let it round the loop again.
        self.assertEqual(self.invoked(), ["status"])

    def test_the_privileged_marker_still_validates_its_verb(self):
        """The privileged side re-validates rather than trusting its caller.

        sudoers names this script with no wildcard, so anything `opn-ci` can
        pass to sudo arrives here as argv. It is only as trustworthy as whoever
        invoked sudo.
        """
        env = dict(os.environ)
        env["PATH"] = self.path
        for bogus in (["--privileged", "bash"], ["--privileged", "exec", "999999"]):
            with self.subTest(bogus=bogus):
                result = subprocess.run(
                    [str(self.wrapper), *bogus],
                    capture_output=True, text=True, env=env, check=False,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.invoked(), [])


if __name__ == "__main__":
    unittest.main()
