#!/usr/bin/env bash
#
# regenerate-demo.sh — rebuild docs/demo.md against the current source.
#
# docs/demo.md is a Showboat document: every ```output block is the real output
# of the ```bash block above it. That only stays true if it is regenerated when
# behaviour changes, and it went five months without — long enough to advertise
# a captured coat shape the proxy had stopped producing.
#
# Run this after changing anything the demo shows: CLI flags, help text, capture
# behaviour, log lines, or the coat file format.
#
#   scripts/regenerate-demo.sh           # regenerate in place
#   scripts/regenerate-demo.sh --check   # report drift, change nothing
#
# Showboat itself is run with `uvx showboat@latest`, so there is nothing to
# install first beyond uv. Whenever the answer is anything but "no drift", its
# own output is kept in demo-verify.log rather than thrown away with the work
# directory.
#
# The parts that are easy to get wrong, and why this is a script:
#
#   - It must run against a build of the working tree, not whatever `trenchcoat`
#     happens to be on PATH. A stale binary silently records stale output.
#   - The demo `cat`s five fixture files and serves them. They live in
#     docs/demo-fixtures/ and are copied into a throwaway directory, because the
#     run also writes captured coats and would otherwise litter the repo.
#   - TZ=UTC, because the document's log lines are timestamped and it has always
#     used UTC. Without it the whole document churns to local time.
#
# --check compares only what a rerun cannot legitimately change. Log timestamps,
# the document's generation stamp and id, and the HTTP Date header inside a
# captured coat all differ on every run and are filtered out, so a clean run
# says so and exits 0 rather than crying wolf.

set -euo pipefail

check_only=false

usage() {
	awk 'NR < 3 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--check)
		check_only=true
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "error: unknown argument '$1'" >&2
		echo "run '$0 --help' for usage" >&2
		exit 2
		;;
	esac
done

command -v uv >/dev/null 2>&1 || {
	echo "error: uv is not on PATH. Showboat runs via 'uvx showboat@latest', so" >&2
	echo "uv is the only prerequisite. See https://docs.astral.sh/uv/" >&2
	echo "" >&2
	echo "docs/demo.md must be regenerated, never hand-edited: its output blocks" >&2
	echo "are only worth anything if a command actually produced them." >&2
	exit 1
}

# The demo's blocks pipe through jq and drive curl. Missing, the rerun records
# "jq: command not found" into docs/demo.md as faithfully as it records real
# output -- a document asserting something no command produced, which is the
# whole failure it exists to prevent.
for tool in jq curl; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "error: $tool is not on PATH, and the demo's commands use it." >&2
		echo "The rerun would record the shell's 'command not found' as though" >&2
		echo "trenchcoat had printed it." >&2
		exit 1
	}
done

cd "$(dirname "$0")/.."

DOC="docs/demo.md"
FIXTURES="docs/demo-fixtures"
VERIFY_LOG="demo-verify.log"

[ -f "$DOC" ] || {
	echo "error: $DOC not found" >&2
	exit 1
}
[ -d "$FIXTURES" ] || {
	echo "error: $FIXTURES not found — the demo cannot run without its coat files" >&2
	exit 1
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "Building trenchcoat from the working tree..."
go build -o "$work/bin/trenchcoat" ./cmd/trenchcoat/

cp "$FIXTURES"/*.yaml "$work/"

# Showboat's own output is the only account of what it did, and $work is removed
# on exit, so keep a copy anywhere it might be wanted.
keep_log() {
	cp "$work/verify.log" "$VERIFY_LOG" 2>/dev/null || true
}

echo "Re-running every block in $DOC..."
rc=0
TZ=UTC PATH="$work/bin:$PATH" uvx --quiet showboat@latest verify "$DOC" \
	--workdir "$work" \
	--output "$work/regenerated.md" >"$work/verify.log" 2>&1 || rc=$?

# showboat verify exits 1 whenever the rerun differs from the recording, and the
# log timestamps guarantee it always does, so 0 and 1 are both normal here --
# the verdict comes from the filtered diff below. Anything else is showboat or
# uv itself failing, and its output is the only clue there is. Discarding the
# status meant a broken Showboat surfaced as a huge spurious diff with no log,
# and, on the regenerate path, as a partial document copied over $DOC.
if [ "$rc" -ne 0 ] && [ "$rc" -ne 1 ]; then
	keep_log
	echo "error: showboat exited $rc, which is neither success nor a difference." >&2
	echo "Last 20 lines of its output:" >&2
	tail -20 "$work/verify.log" >&2
	echo "" >&2
	echo "Full output: $VERIFY_LOG. $DOC has not been touched." >&2
	exit 1
fi

[ -s "$work/regenerated.md" ] || {
	keep_log
	echo "error: showboat produced no document. Last 20 lines of its output:" >&2
	tail -20 "$work/verify.log" >&2
	echo "" >&2
	echo "Full output: $VERIFY_LOG" >&2
	exit 1
}

# Mask everything that necessarily differs between two runs, so what is left is
# behaviour that actually changed: the timestamp on each slog line, the
# document's own generation stamp and id, and the HTTP Date header recorded
# inside captured coats. Without that last one --check reports drift on every
# run and stops meaning anything.
#
# These are substitutions, not deletions. Dropping a whole line exempts
# everything else on it from the diff, and slog lines are the most volatile
# thing here and the most worth checking: a `--verbose` log line is a documented
# CLI surface, and roughly twenty of them are recorded in the document. Deleting
# them meant renaming a log message registered as no drift at all.
filter() {
	sed -E \
		-e 's/^time=[^ ]+ /time=<TS> /' \
		-e 's/^\*[0-9]{4}-.*\*$/*<GENERATED>*/' \
		-e 's/^<!-- showboat-id:.*/<!-- showboat-id: <ID> -->/' \
		-e 's/^([[:space:]]*Date: )[A-Z][a-z]{2},.*/\1<DATE>/' \
		"$1"
}

if filter "$DOC" | diff -q - <(filter "$work/regenerated.md") >/dev/null; then
	echo "No behavioural drift — only timestamps differ."
	drifted=false
else
	echo ""
	echo "Behavioural drift (timestamps and document id excluded):"
	echo ""
	filter "$DOC" | diff -u - <(filter "$work/regenerated.md") |
		sed -e "s|^--- -|--- $DOC (recorded)|" -e 's|^+++ /dev/fd.*|+++ (current behaviour)|' || true
	keep_log
	echo ""
	echo "Showboat's own account of the rerun: $VERIFY_LOG"
	drifted=true
fi

if [ "$check_only" = true ]; then
	echo ""
	echo "--check: $DOC not modified."
	if [ "$drifted" = true ]; then
		exit 1
	fi
	exit 0
fi

cp "$work/regenerated.md" "$DOC"
echo ""
echo "Regenerated $DOC"
