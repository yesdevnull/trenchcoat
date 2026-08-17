#!/usr/bin/env python3
"""PostToolUse hook: format a Go file immediately after Claude edits it.

CLAUDE.md requires gofmt and goimports to be clean before every commit, and the
CI "Format" job fails the build on any file they would change. Running both on
the single file that was just edited keeps that promise without anyone having to
remember it.

Reads a PostToolUse payload on stdin and formats tool_input.file_path in place.
Non-Go files, files that no longer exist, and missing formatters are all no-ops:
a formatter is not worth interrupting the session over, and CI still backstops.

Test with: python3 .claude/hooks/test_hooks.py
"""

from __future__ import annotations  # hooks may run under macOS system Python 3.9

import json
import os
import shutil
import subprocess
import sys

FORMATTERS = ("gofmt", "goimports")


def gopath_bin() -> str:
    """Best effort GOPATH/bin, without assuming the default GOPATH.

    CI images and devcontainers routinely relocate GOPATH, so ~/go/bin is a
    guess of last resort rather than the answer. `go env` is consulted only
    when the environment does not already say.
    """
    gopath = os.environ.get("GOPATH", "")
    if not gopath:
        try:
            result = subprocess.run(
                ["go", "env", "GOPATH"],
                capture_output=True,
                text=True,
                check=False,
            )
            if result.returncode == 0:
                gopath = result.stdout.strip()
        except OSError:
            gopath = ""
    if not gopath:
        gopath = os.path.expanduser(os.path.join("~", "go"))
    return os.path.join(gopath, "bin")


def find_formatter(name: str) -> str | None:
    """Locate a formatter, falling back to GOPATH/bin.

    Hooks do not inherit an interactive shell's PATH, so goimports -- which is
    installed by `go install` rather than a package manager -- is routinely
    absent from PATH here even though it works fine in a terminal.
    """
    found = shutil.which(name)
    if found:
        return found
    fallback = os.path.join(gopath_bin(), name)
    return fallback if os.access(fallback, os.X_OK) else None


def main() -> int:
    try:
        payload = json.loads(sys.stdin.read())
    except (json.JSONDecodeError, ValueError):
        return 0

    path = (payload.get("tool_input") or {}).get("file_path") or ""
    if not path.endswith(".go") or not os.path.isfile(path):
        return 0

    for name in FORMATTERS:
        formatter = find_formatter(name)
        if formatter is None:
            continue
        subprocess.run(
            [formatter, "-w", path],
            capture_output=True,
            text=True,
            check=False,
        )

    return 0


if __name__ == "__main__":
    sys.exit(main())
