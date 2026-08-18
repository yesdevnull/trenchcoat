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
# install first beyond uv.
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

cd "$(dirname "$0")/.."

DOC="docs/demo.md"
FIXTURES="docs/demo-fixtures"

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

echo "Re-running every block in $DOC..."
TZ=UTC PATH="$work/bin:$PATH" uvx --quiet showboat@latest verify "$DOC" \
	--workdir "$work" \
	--output "$work/regenerated.md" >"$work/verify.log" 2>&1 || true

[ -s "$work/regenerated.md" ] || {
	echo "error: showboat produced no document. Last 20 lines of its output:" >&2
	tail -20 "$work/verify.log" >&2
	exit 1
}

# Ignore everything that necessarily differs between two runs, so what is left
# is behaviour that actually changed: slog output, the document's own generation
# stamp and id, and the HTTP Date header recorded inside captured coats. Without
# that last one --check reports drift on every run and stops meaning anything.
filter() {
	grep -vE '^time=|^\*[0-9]{4}-|^<!-- showboat-id:|^[[:space:]]*Date: [A-Z][a-z]{2}, ' "$1"
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
