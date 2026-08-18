#!/usr/bin/env python3
"""Tests for the scripts in scripts/.

Run with:

    python3 scripts/test_scripts.py

Both scripts are exercised end to end, against real tools, in throwaway
directories -- nothing is mocked, for the same reason .claude/hooks/test_hooks.py
is not: the value of these scripts is that they agree with go, showboat and the
tree they are pointed at.

Each script does `cd "$(dirname "$0")/.."`, so a copy of the script dropped into
`<tmp>/scripts/` treats `<tmp>` as the repository. The fixtures below are the
smallest thing that shape will accept: a Go module with one package for
coverage-report.sh, and a module whose ./cmd/trenchcoat prints a single slog line
plus a one-block Showboat document for regenerate-demo.sh.

Written to run on Python 3.9, like the hooks -- macOS ships 3.9.6.
"""

from __future__ import annotations  # match the hooks: runnable on Python 3.9

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
COVERAGE_REPORT = SCRIPTS / "coverage-report.sh"
REGENERATE_DEMO = SCRIPTS / "regenerate-demo.sh"


def require(tool: str) -> None:
    """Skip rather than fail when a prerequisite is absent locally.

    CI installs both, so nothing is skipped there.
    """
    if shutil.which(tool) is None:
        raise unittest.SkipTest("%s is not on PATH" % tool)


def run(script: Path, *args: str, env: dict = None):
    return subprocess.run(
        [str(script), *args], capture_output=True, text=True, env=env
    )


TINY_GO_MOD = "module example.com/tiny\n\ngo 1.21\n"

# AlwaysCalled is exercised by the test below; NeverCalled is not, so the
# package sits under any threshold and `--min` has exactly one thing to list.
TINY_GO = """package tiny

func AlwaysCalled(a, b int) int {
	return a + b
}

func NeverCalled() string {
	return "no test reaches this"
}
"""

TINY_TEST_GO = """package tiny

import "testing"

func TestAlwaysCalled(t *testing.T) {
	if AlwaysCalled(1, 2) != 3 {
		t.Fatal("arithmetic broke")
	}
}
"""

FAILING_TEST_GO = """package tiny

import "testing"

func TestAlwaysCalled(t *testing.T) {
	t.Fatal("deliberately failing so the script has something to report")
}
"""

# The failure comes first and forty lines of chatter follow it, the way a real
# suite's slog output does. Anything that only shows the tail of the log loses
# the verdict entirely.
NOISY_FAILING_TEST_GO = """package tiny

import (
	"fmt"
	"testing"
)

func TestAlwaysCalled(t *testing.T) {
	t.Fatal("deliberately failing so the script has something to report")
}

func TestNoisy(t *testing.T) {
	for i := 0; i < 40; i++ {
		fmt.Printf("level=INFO msg=\\"chatter\\" line=%d\\n", i)
	}
}
"""


class CoverageReportArgumentsTest(unittest.TestCase):
    """Argument validation, which happens before the script touches anything.

    A `--min` the script cannot read is the one input that must not be waved
    through: awk coerces a non-numeric limit to 0, which lists no functions at
    all and reads as "everything is covered".
    """

    def test_min_rejects_a_non_numeric_percentage(self):
        result = run(COVERAGE_REPORT, "--min", "ninety")

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("--min takes a percentage", result.stderr)

    def test_min_rejects_a_percentage_above_100(self):
        result = run(COVERAGE_REPORT, "--min", "101")

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("between 0 and 100", result.stderr)

    def test_min_requires_a_value(self):
        result = run(COVERAGE_REPORT, "--min")

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("--min needs a percentage", result.stderr)

    def test_unknown_argument_is_rejected(self):
        result = run(COVERAGE_REPORT, "--functons")

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("unknown argument", result.stderr)


class CoverageReportRunTest(unittest.TestCase):
    """The script run against a real, tiny Go module."""

    def setUp(self):
        require("go")
        self.repo = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, str(self.repo))
        (self.repo / "scripts").mkdir()
        shutil.copy(str(COVERAGE_REPORT), str(self.repo / "scripts"))
        (self.repo / "go.mod").write_text(TINY_GO_MOD)
        (self.repo / "tiny.go").write_text(TINY_GO)
        (self.repo / "tiny_test.go").write_text(TINY_TEST_GO)

    def script(self) -> Path:
        return self.repo / "scripts" / COVERAGE_REPORT.name

    def test_lists_functions_below_a_fractional_threshold(self):
        """`go tool cover` reports one decimal place, so --min must take one."""
        result = run(self.script(), "--min", "99.5")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Functions below 99.5%", result.stdout)
        self.assertIn("NeverCalled", result.stdout)
        self.assertNotIn("AlwaysCalled", result.stdout)

    def test_fails_loudly_when_the_test_suite_fails(self):
        """Coverage numbers from a failing suite would be a lie, so refuse."""
        (self.repo / "tiny_test.go").write_text(FAILING_TEST_GO)

        result = run(self.script())

        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("the test suite failed", result.stderr)
        self.assertIn("coverage-test.log", result.stderr)
        self.assertNotIn("Total:", result.stdout)

    def test_names_the_failing_test_under_a_pile_of_output(self):
        """A noisy failure must not push the verdict out of the report."""
        (self.repo / "tiny_test.go").write_text(NOISY_FAILING_TEST_GO)

        result = run(self.script())

        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("--- FAIL: TestAlwaysCalled", result.stderr)


FAKE_TRENCHCOAT_GO = """package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewTextHandler(os.Stdout, nil)).Info(%s, "count", 2)
}
"""


class RegenerateDemoTest(unittest.TestCase):
    """--check against a throwaway repository whose demo is one slog line.

    The line is the point. Its timestamp differs on every run and must be
    masked, while the rest of it must still be compared -- a filter that dropped
    whole `time=` lines exempted every log line in docs/demo.md from the check,
    so renaming a log message registered as no drift at all.
    """

    @classmethod
    def setUpClass(cls):
        require("go")
        require("uv")
        cls.pristine = Path(tempfile.mkdtemp())
        cls.addClassCleanup(shutil.rmtree, str(cls.pristine))
        cls.build_repo(cls.pristine)

    @classmethod
    def build_repo(cls, repo: Path) -> None:
        (repo / "cmd" / "trenchcoat").mkdir(parents=True)
        (repo / "docs" / "demo-fixtures").mkdir(parents=True)
        (repo / "scripts").mkdir()
        (repo / "bin").mkdir()
        (repo / "gen").mkdir()

        shutil.copy(str(REGENERATE_DEMO), str(repo / "scripts"))
        (repo / "go.mod").write_text("module example.com/demo\n\ngo 1.21\n")
        (repo / "cmd" / "trenchcoat" / "main.go").write_text(
            FAKE_TRENCHCOAT_GO % '"coats loaded"'
        )
        # The script copies docs/demo-fixtures/*.yaml into its work directory;
        # the document below does not read them, but the copy must find one.
        (repo / "docs" / "demo-fixtures" / "basic.yaml").write_text("coats: []\n")

        subprocess.run(
            ["go", "build", "-o", "bin/trenchcoat", "./cmd/trenchcoat/"],
            cwd=str(repo),
            check=True,
            capture_output=True,
        )

        # Record the document with the same Showboat the script re-runs it with.
        doc = str(repo / "docs" / "demo.md")
        cls.showboat(repo, "init", doc, "Test Demo")
        cls.showboat(repo, "exec", doc, "bash", "trenchcoat")

    @classmethod
    def showboat(cls, repo: Path, *args: str) -> None:
        env = dict(os.environ)
        env["TZ"] = "UTC"
        env["PATH"] = str(repo / "bin") + os.pathsep + env["PATH"]
        subprocess.run(
            ["uvx", "--quiet", "showboat@latest", *args],
            cwd=str(repo / "gen"),
            check=True,
            capture_output=True,
            env=env,
        )

    def setUp(self):
        self.repo = Path(tempfile.mkdtemp()) / "repo"
        self.addCleanup(shutil.rmtree, str(self.repo.parent))
        shutil.copytree(str(self.pristine), str(self.repo))

    def check(self):
        return run(self.repo / "scripts" / REGENERATE_DEMO.name, "--check")

    def test_reports_no_drift_when_only_the_timestamp_differs(self):
        result = self.check()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("No behavioural drift", result.stdout)

    def test_reports_a_changed_log_message_as_drift(self):
        (self.repo / "cmd" / "trenchcoat" / "main.go").write_text(
            FAKE_TRENCHCOAT_GO % '"coats loaded from disk"'
        )

        result = self.check()

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("Behavioural drift", result.stdout)
        self.assertIn('msg="coats loaded from disk"', result.stdout)
        self.assertIn("not modified", result.stdout)

    def test_refuses_to_run_without_jq(self):
        """The demo's blocks pipe through jq.

        Without it the rerun records "jq: command not found" into the document
        as though the binary had printed it -- a regenerated document asserting
        something no command produced.
        """
        # A PATH holding only what the script needs to reach the guard: bash for
        # its own shebang, and uv for the check ahead of this one.
        bin_dir = self.repo / "minimal-path"
        bin_dir.mkdir()
        for tool in ("bash", "uv"):
            (bin_dir / tool).symlink_to(shutil.which(tool))
        env = dict(os.environ)
        env["PATH"] = str(bin_dir)

        result = run(self.repo / "scripts" / REGENERATE_DEMO.name, "--check", env=env)

        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("jq", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
