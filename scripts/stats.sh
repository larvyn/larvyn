#!/bin/sh
# Download stats for Larvyn releases, as a Markdown table.
#
#   scripts/stats.sh                 from the GitHub API (needs gh)
#   scripts/stats.sh snapshot.csv    from a "tag,asset,downloads" file
#
# GitHub counts every download of a release file. The install scripts fetch the
# platform file and installs.txt; the in-app updater fetches only the platform
# file. So per release:
#   installs = downloads of installs.txt   (from the release that added it on)
#   updates  = platform downloads - installs
# The counts only grow, include re-installs and our own tests, and are not
# unique people. The daily snapshots on the `stats` branch give the trend.
set -eu

REPO="${LARVYN_RELEASES_REPO:-larvyn/releases}"

if [ $# -gt 0 ]; then
	rows=$(cat "$1")
else
	rows=$(gh api --paginate "repos/$REPO/releases" --jq '.[] | .tag_name as $t | .assets[] | "\($t),\(.name),\(.download_count)"')
fi

printf '%s\n' "$rows" | awk -F, '
	$1 == "tag" { next }
	{
		if (!($1 in seen)) { seen[$1] = 1; order[++n] = $1 }
		if ($2 ~ /darwin/) mac[$1] += $3
		else if ($2 ~ /linux/) linux[$1] += $3
		else if ($2 ~ /windows/) win[$1] += $3
		else if ($2 == "installs.txt") { inst[$1] = $3; counted[$1] = 1 }
	}
	END {
		print "| Release | macOS | Linux | Windows | Downloads | Installs | Updates |"
		print "|---|---:|---:|---:|---:|---:|---:|"
		for (i = 1; i <= n; i++) {
			t = order[i]
			all = mac[t] + linux[t] + win[t]
			if (t in counted) { ins = inst[t]; upd = all - ins; ti += ins; tu += upd; i_s = ins; u_s = upd }
			else { i_s = "–"; u_s = "–" }
			printf "| %s | %d | %d | %d | %d | %s | %s |\n", t, mac[t], linux[t], win[t], all, i_s, u_s
			tm += mac[t]; tl += linux[t]; tw += win[t]
		}
		printf "| **Total** | **%d** | **%d** | **%d** | **%d** | **%d** | **%d** |\n", tm, tl, tw, tm + tl + tw, ti, tu
	}'
