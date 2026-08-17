#!/usr/bin/env python3
"""PostToolUse hook: warn when the coat types drift from coatfile.schema.json.

coatfile.schema.json hand-mirrors the coat schema defined by internal/coat's
types and validation rules. Nothing in the build or test suite enforces that the
two agree, and they have silently diverged before -- commit 46ff686 ("schema
sync") fixed one such drift that only a human reviewer caught.

So: whenever internal/coat/types.go or internal/coat/validate.go is edited and
the schema is *not* also dirty in the working tree, tell Claude about it. The
warning clears itself the moment the schema is touched, so it nags exactly until
the job is done.

Reads a PostToolUse payload on stdin. Emits nothing at all unless the drift is
real -- a hook that fires on every edit is a hook people learn to ignore.

Test with: python3 .claude/hooks/test_hooks.py
"""

from __future__ import annotations  # hooks may run under macOS system Python 3.9

import json
import os
import subprocess
import sys
from pathlib import PurePath

SCHEMA = "coatfile.schema.json"
WATCHED = ("internal/coat/types.go", "internal/coat/validate.go")

WARNING = (
    f"{{edited}} was edited but {SCHEMA} is unchanged.\n"
    f"{SCHEMA} hand-mirrors the coat schema and nothing enforces that they "
    f"agree. If this edit added, removed or renamed a coat field -- or changed "
    f"a validation rule that the schema encodes -- update {SCHEMA} in the same "
    f"commit. If the edit does not touch the schema surface, ignore this."
)


def git(repo: str, *args: str) -> str | None:
    """Run a read-only git command, returning stdout or None if it fails."""
    try:
        result = subprocess.run(
            ["git", "-C", repo, *args],
            capture_output=True,
            text=True,
            check=False,
        )
    except OSError:
        return None
    return result.stdout if result.returncode == 0 else None


def schema_is_dirty(repo_root: str) -> bool:
    """True when the schema has uncommitted changes, staged or not.

    Limiting status to the one pathspec means git answers the question directly,
    with no porcelain output to hand-parse -- rename arrows and C-quoted names
    included. --no-optional-locks keeps this from refreshing (and locking) the
    index behind a git command the human is running at the same time.
    """
    porcelain = git(
        repo_root, "--no-optional-locks", "status", "--porcelain", "--", SCHEMA
    )
    return bool(porcelain and porcelain.strip())


def main() -> int:
    try:
        payload = json.loads(sys.stdin.read())
    except (json.JSONDecodeError, ValueError):
        return 0

    path = (payload.get("tool_input") or {}).get("file_path") or ""
    if not path:
        return 0

    # realpath both sides: on macOS the temp and repo paths routinely differ by
    # the /var -> /private/var symlink, which would break the relpath compare.
    path = os.path.realpath(path)
    root = git(os.path.dirname(path) or ".", "rev-parse", "--show-toplevel")
    if root is None:
        return 0
    repo_root = os.path.realpath(root.strip())

    # as_posix(): relpath yields OS-native separators, so on Windows this would
    # be internal\coat\types.go and never match the forward-slash WATCHED entries.
    relative = PurePath(os.path.relpath(path, repo_root)).as_posix()
    if relative not in WATCHED:
        return 0

    if schema_is_dirty(repo_root):
        return 0

    print(
        json.dumps(
            {
                "hookSpecificOutput": {
                    "hookEventName": "PostToolUse",
                    "additionalContext": WARNING.format(edited=relative),
                }
            }
        ),
        flush=True,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
