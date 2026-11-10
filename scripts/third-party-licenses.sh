#!/bin/bash
# Prints the licenses of what a cassetty for <os>/<arch> is built from
# besides its own code: the Go modules compiled into it, each with the
# license files of the release in use, and Go itself, whose runtime and
# standard library are part of every Go program. Nearly all of these
# licenses ask that their text goes with a copy in binary form, so the
# release archives carry it as THIRD_PARTY_LICENSES.
#
# The modules differ between the systems: Linux has the ones for D-Bus and
# PulseAudio. They must be in the module cache, as they are after a build.
#
# Run by scripts/release-build.sh.
#
# usage: scripts/third-party-licenses.sh <os> <arch>
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: $0 <os> <arch>" >&2
	exit 2
fi
cd "$(dirname "$0")/.."
export GOOS=$1 GOARCH=$2 CGO_ENABLED=0

rule=$(printf '=%.0s' {1..78})
section() {
	printf '%s\n%s\n%s\n\n' "$rule" "$1" "$rule"
}

cat <<END
cassetty is built from the software below. Each is the work of its authors
and comes under its own license, which follows its name.

END

go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' . |
	sort -u |
	while read -r module version dir; do
		found=no
		for file in "$dir"/*; do
			case $(basename "$file" | tr '[:lower:]' '[:upper:]') in
			LICENSE* | LICENCE* | COPYING* | UNLICENSE* | NOTICE* | PATENTS*)
				section "$module $version: $(basename "$file")"
				cat "$file"
				printf '\n\n'
				found=yes
				;;
			esac
		done
		if [ $found = no ]; then
			echo "$0: $module $version has no license file" >&2
			exit 1
		fi
	done

# An installed Go has its license at its root; Homebrew keeps it one up.
goroot=$(go env GOROOT)
for file in "$goroot/LICENSE" "$goroot/../LICENSE"; do
	if [ -f "$file" ]; then
		section "Go $(go env GOVERSION), its runtime and standard library: LICENSE"
		cat "$file"
		exit 0
	fi
done
echo "$0: no license of Go in $goroot" >&2
exit 1
