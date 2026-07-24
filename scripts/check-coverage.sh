#!/bin/sh
set -eu

profile=${1:-coverage.out}
minimum=${COVERAGE_MIN:-75.0}
coverage=$(go tool cover -func="$profile" | awk '/^total:/ { gsub("%", "", $3); print $3 }')

if [ -z "$coverage" ]; then
	echo "coverage total not found in $profile" >&2
	exit 1
fi

printf 'Total coverage: %s%% (minimum %s%%)\n' "$coverage" "$minimum"
awk -v actual="$coverage" -v minimum="$minimum" 'BEGIN { exit !(actual + 0 >= minimum + 0) }'
