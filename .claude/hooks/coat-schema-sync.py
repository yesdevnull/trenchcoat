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

import json
import os
import subprocess
import sys

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


def dirty_paths(repo_root: str) -> set[str]:
    """Repo-relative paths with uncommitted changes."""
    porcelain = git(repo_root, "status", "--porcelain")
    if porcelain is None:
        return set()

    paths = set()
    for line in porcelain.splitlines():
        if not line.strip():
            continue
        # Format is "XY <path>", and renames appear as "old -> new".
        path = line[3:].strip()
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        paths.add(path.strip('"'))
    return paths


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

    relative = os.path.relpath(path, repo_root)
    if relative not in WATCHED:
        return 0

    if SCHEMA in dirty_paths(repo_root):
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
