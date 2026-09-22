#!/usr/bin/env bash
# Sync published intra-repo require versions to the latest module tags.
#
# Local replace directives can hide a stale required version. Published users,
# however, receive exactly the version in the require line. This script keeps
# those versions aligned with the newest tags:
#
#   scripts/sync-deps.sh          # rewrite stale requires and run go mod tidy
#   scripts/sync-deps.sh --check  # report drift without changing files (CI)
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

check_only=false
case "${1:-}" in
	--check) check_only=true ;;
	"") ;;
	*) echo "Usage: $0 [--check]"; exit 1 ;;
esac

latest_version() {
	local dir="$1"
	git tag -l "${dir}/v*" |
		grep -E "^${dir}/v[0-9]+\.[0-9]+\.[0-9]+$" |
		sed "s|^${dir}/||" |
		sort -V | tail -n1
}

internal_requires() {
	awk '
		/^require [(]/  { block=1; next }
		block && /^[)]/ { block=0; next }
		block           { line=$0 }
		/^require [^(]/ { line=$0; sub(/^require /,"",line) }
		line ~ /github\.com\/joakimcarlsson\/minmux\// {
			n=split(line, fields, " ")
			if (fields[1] ~ /^github\.com\/joakimcarlsson\/minmux\// &&
			    fields[2] ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/)
				print fields[1] "\t" fields[2]
			line=""
		}
	' "$1"
}

drift=0
fixed=0

while IFS= read -r mod; do
	[[ -z "$mod" || "$mod" == examples/* || "$mod" == tests/* ]] && continue
	gomod="${REPO_ROOT}/${mod}/go.mod"
	changed=false

	while IFS=$'\t' read -r path have; do
		[[ -z "$path" ]] && continue
		dep_dir="${path#github.com/joakimcarlsson/minmux/}"
		want="$(latest_version "$dep_dir")"
		[[ -z "$want" || "$have" == "$want" ]] && continue
		[[ "$(printf '%s\n%s\n' "$have" "$want" | sort -V | tail -n1)" == "$have" ]] && continue
		drift=$((drift + 1))
		echo "DRIFT  $mod: $path  $have -> $want"
		if [[ "$check_only" == false ]]; then
			(cd "$mod" && go mod edit -require="${path}@${want}")
			changed=true
		fi
	done < <(internal_requires "$gomod")

	if [[ "$changed" == true ]]; then
		(cd "$mod" && GOWORK=off go mod tidy)
		fixed=$((fixed + 1))
	fi
done < <(scripts/release.sh modules)

if [[ "$check_only" == true ]]; then
	if [[ "$drift" -gt 0 ]]; then
		echo
		echo "Found $drift stale intra-repo require(s). Run: scripts/sync-deps.sh"
		exit 1
	fi
	echo "No intra-repo version drift."
else
	echo
	echo "Synced $drift require line(s) across $fixed module(s)."
fi
