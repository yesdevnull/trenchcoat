#!/usr/bin/env bash
#
# coverage-report.sh — coverage for the trenchcoat test suite, on demand.
#
# This replaces the coverage report that used to be checked in at
# docs/test-coverage-analysis.md. A committed report is wrong the moment
# anyone adds a test, and that one drifted five months before anyone noticed:
# it claimed 184 tests when there were 314, listed packages that no longer
# existed, and named gaps that had already been filled. Generate it when you
# want it instead.
#
# Use it when you want to know what a change did to coverage, or before
# deciding where the next test should go.
#
#   scripts/coverage-report.sh              # per-package table and total
#   scripts/coverage-report.sh --functions  # also list functions under 100%
#   scripts/coverage-report.sh --min 90     # only functions under 90%
#   scripts/coverage-report.sh --html       # also write coverage.html
#
# Test output goes to a log rather than the terminal, because the point here is
# the coverage numbers. On failure the tail is printed and the log path named.

set -euo pipefail

PROFILE="coverage.out"
HTML="coverage.html"
LOG="coverage-test.log"

show_functions=false
min_coverage=100
write_html=false

# The header comment is the help text, so the two cannot drift apart. Skip the
# shebang, print comment lines with their marker stripped, stop at the first
# line of actual script.
usage() {
	awk 'NR < 3 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--functions)
		show_functions=true
		shift
		;;
	--min)
		[ $# -ge 2 ] || {
			echo "error: --min needs a percentage, e.g. --min 90" >&2
			exit 2
		}
		min_coverage="$2"
		show_functions=true
		shift 2
		;;
	--html)
		write_html=true
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

command -v go >/dev/null 2>&1 || {
	echo "error: go is not on PATH — see the Requirements section of CLAUDE.md" >&2
	exit 1
}

cd "$(dirname "$0")/.."

echo "Running tests with the race detector and coverage — this takes a moment..."
if ! go test -count=1 -race -coverprofile="$PROFILE" ./... >"$LOG" 2>&1; then
	echo "" >&2
	echo "error: the test suite failed, so the coverage numbers below would be a lie." >&2
	echo "Last 20 lines of $LOG:" >&2
	echo "" >&2
	tail -20 "$LOG" >&2
	echo "" >&2
	echo "Full output: $LOG" >&2
	exit 1
fi

echo ""
echo "Coverage by package"
echo "-------------------"
# `go test` prints a coverage line per package; the log is the only place that
# survives, since the run above was redirected.
grep -E '^(ok|---)' "$LOG" | sed -E 's/[[:space:]]+/ /g' | sort

echo ""
total=$(go tool cover -func="$PROFILE" | awk '$1 == "total:" { print $NF }')
echo "Total: $total"

if [ "$show_functions" = true ]; then
	echo ""
	echo "Functions below ${min_coverage}%"
	echo "---------------------------"
	go tool cover -func="$PROFILE" |
		awk -v limit="$min_coverage" '
			$1 != "total:" {
				pct = $NF
				sub(/%$/, "", pct)
				if (pct + 0 < limit + 0) printf "%7s  %-28s %s\n", $NF, $2, $1
			}
		' |
		sort -n
fi

if [ "$write_html" = true ]; then
	go tool cover -html="$PROFILE" -o "$HTML"
	echo ""
	echo "HTML report: $HTML"
fi

echo ""
echo "Profile: $PROFILE   Test log: $LOG"
