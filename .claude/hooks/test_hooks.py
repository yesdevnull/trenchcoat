#!/usr/bin/env python3
"""Tests for the trenchcoat Claude Code hooks.

Run with:

    python3 .claude/hooks/test_hooks.py

The hooks are exercised end to end: each test spawns the real hook script with a
real PostToolUse payload on stdin and asserts on the files it touched and the
JSON it emitted. Real gofmt, goimports and git are used -- nothing is mocked,
because the whole value of these hooks is that they agree with the tools CI
runs.
"""

from __future__ import annotations  # match the hooks: runnable on Python 3.9

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

HOOKS_DIR = Path(__file__).resolve().parent
GOFMT_HOOK = HOOKS_DIR / "gofmt-on-write.py"
SCHEMA_HOOK = HOOKS_DIR / "coat-schema-sync.py"


def run_hook(
    hook: Path,
    file_path: str,
    cwd: str | None = None,
    env: dict[str, str] | None = None,
):
    """Invoke a hook with a PostToolUse payload, returning the CompletedProcess."""
    payload = json.dumps(
        {
            "hook_event_name": "PostToolUse",
            "tool_name": "Edit",
            "tool_input": {"file_path": file_path},
        }
    )
    return subprocess.run(
        [str(hook)],
        input=payload,
        capture_output=True,
        text=True,
        cwd=cwd,
        env=env,
    )


def git(repo: str, *args: str) -> None:
    """Run a git command in repo with a self-contained identity.

    Signing is forced off so the tests never reach for Dan's signing key.
    """
    subprocess.run(
        [
            "git",
            "-C",
            repo,
            "-c",
            "user.email=test@example.com",
            "-c",
            "user.name=Test",
            "-c",
            "commit.gpgsign=false",
            *args,
        ],
        check=True,
        capture_output=True,
    )


UNFORMATTED_GO = "package main\nfunc  main( ) {\nx:=1\n_ = x\n}\n"
UNUSED_IMPORT_GO = 'package main\n\nimport "os"\n\nfunc main() {}\n'


class GofmtOnWriteTest(unittest.TestCase):
    """The formatter hook keeps edited Go files in the shape CI demands."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp)

    def write(self, name: str, content: str) -> str:
        path = os.path.join(self.tmp, name)
        Path(path).write_text(content)
        return path

    def test_formats_unformatted_go_file(self):
        path = self.write("main.go", UNFORMATTED_GO)

        result = run_hook(GOFMT_HOOK, path)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("func main() {", Path(path).read_text())

    def test_removes_unused_import(self):
        """Proves goimports runs, not just gofmt -- gofmt leaves this alone."""
        path = self.write("unused.go", UNUSED_IMPORT_GO)

        result = run_hook(GOFMT_HOOK, path)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('"os"', Path(path).read_text())

    def test_leaves_non_go_files_untouched(self):
        original = "#  Heading\n\n\n   ragged    text\n"
        path = self.write("notes.md", original)

        result = run_hook(GOFMT_HOOK, path)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(Path(path).read_text(), original)

    def test_missing_file_is_not_an_error(self):
        result = run_hook(GOFMT_HOOK, os.path.join(self.tmp, "gone.go"))

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_finds_goimports_under_a_non_default_gopath(self):
        """goimports lives in GOPATH/bin, which is not always ~/go/bin.

        PATH is stripped to the system default and HOME is redirected at an
        empty directory, so the only way to reach the formatter is by honouring
        GOPATH. CI images and devcontainers routinely set it elsewhere.
        """
        gopath = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, gopath)
        empty_home = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, empty_home)

        bin_dir = Path(gopath) / "bin"
        bin_dir.mkdir()
        sentinel = Path(gopath) / "goimports-ran"
        fake = bin_dir / "goimports"
        fake.write_text(f'#!/bin/sh\necho "$@" > "{sentinel}"\n')
        fake.chmod(0o755)

        path = self.write("main.go", UNFORMATTED_GO)
        result = run_hook(
            GOFMT_HOOK,
            path,
            env={"PATH": "/usr/bin:/bin", "HOME": empty_home, "GOPATH": gopath},
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(
            sentinel.exists(), "goimports in GOPATH/bin was never invoked"
        )

    def test_is_silent_on_success(self):
        """A formatter that chatters on every edit becomes noise to ignore."""
        path = self.write("main.go", UNFORMATTED_GO)

        result = run_hook(GOFMT_HOOK, path)

        self.assertEqual(result.stdout, "")


class CoatSchemaSyncTest(unittest.TestCase):
    """The desync hook nags when the coat types drift from the JSON Schema."""

    def setUp(self):
        self.repo = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.repo)
        git(self.repo, "init")
        coat_dir = Path(self.repo) / "internal" / "coat"
        coat_dir.mkdir(parents=True)
        (coat_dir / "types.go").write_text("package coat\n\ntype Coat struct{}\n")
        (coat_dir / "validate.go").write_text("package coat\n\nfunc Validate() {}\n")
        (Path(self.repo) / "coatfile.schema.json").write_text('{"title": "coat"}\n')
        (Path(self.repo) / "README.md").write_text("# trenchcoat\n")
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-m", "initial")

    def path(self, *parts: str) -> str:
        return os.path.join(self.repo, *parts)

    def touch(self, *parts: str) -> str:
        target = Path(self.path(*parts))
        target.write_text(target.read_text() + "\n// changed\n")
        return str(target)

    def additional_context(self, result) -> str:
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.strip(), "expected JSON on stdout")
        payload = json.loads(result.stdout)
        hook_output = payload["hookSpecificOutput"]
        self.assertEqual(hook_output["hookEventName"], "PostToolUse")
        return hook_output["additionalContext"]

    def test_warns_when_types_change_without_schema(self):
        edited = self.touch("internal", "coat", "types.go")

        result = run_hook(SCHEMA_HOOK, edited, cwd=self.repo)

        self.assertIn("coatfile.schema.json", self.additional_context(result))

    def test_warns_when_validate_changes_without_schema(self):
        edited = self.touch("internal", "coat", "validate.go")

        result = run_hook(SCHEMA_HOOK, edited, cwd=self.repo)

        self.assertIn("coatfile.schema.json", self.additional_context(result))

    def test_silent_when_schema_changed_too(self):
        edited = self.touch("internal", "coat", "types.go")
        self.touch("coatfile.schema.json")

        result = run_hook(SCHEMA_HOOK, edited, cwd=self.repo)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_silent_for_unrelated_file(self):
        edited = self.touch("README.md")

        result = run_hook(SCHEMA_HOOK, edited, cwd=self.repo)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_silent_outside_a_git_repo(self):
        loose = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, loose)
        stray = Path(loose) / "types.go"
        stray.write_text("package coat\n")

        result = run_hook(SCHEMA_HOOK, str(stray), cwd=loose)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main(verbosity=2)
