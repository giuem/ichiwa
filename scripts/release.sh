#!/bin/sh
# 发布；RELEASING.md 说明整个流程。
#
#   scripts/release.sh next          下一个版本号
#   scripts/release.sh tag [-push] [-n] [-trailer "Co-Authored-By: …"]
#                                    发布 CHANGELOG.md 里 Unreleased 下的内容：
#                                    给它下一个版本号，提交，打 tag v<版本>；
#                                    加 -push 就把 main 和 tag 一起推送，
#                                    推送会启动 release workflow；
#                                    -n 只显示会做什么，不改任何东西
#
# 版本号是 YYYY.MDD.N：发布当天（Asia/Shanghai）的年、不补零的月加两位的日、
# 当天的第几次发布（从 1 开始），例如 2026.1107.2。
set -eu

cd "$(dirname "$0")/.."

say() { printf '==> %s\n' "$*" >&2; }
die() {
	printf 'release: %s\n' "$*" >&2
	exit 1
}

today() { TZ=Asia/Shanghai date +%Y.%-m%d; }

# next 打印下一个版本号，数本地和 origin 上今天已有的 tag。
next() {
	day=$(today)
	last=$({
		git tag --list "v$day.*"
		git ls-remote --tags origin "refs/tags/v$day.*" 2>/dev/null | sed 's|.*refs/tags/||; s|\^{}$||'
	} | sed -n "s/^v$(printf '%s' "$day" | sed 's/\./\\./g')\.\([1-9][0-9]*\)$/\1/p" | sort -n | tail -1)
	echo "$day.$(( ${last:-0} + 1 ))"
}

# unreleased 打印 CHANGELOG.md 里 Unreleased 下的内容。
unreleased() {
	awk '/^## / { if (found) exit; found = ($0 == "## Unreleased"); next } found' CHANGELOG.md
}

tag() {
	push=0
	dry=0
	trailer=
	while [ $# -gt 0 ]; do
		case $1 in
		-push) push=1 ;;
		-n) dry=1 ;;
		-trailer)
			shift
			trailer=${1:?-trailer 需要一行内容}
			;;
		*) die "不认识的参数 $1" ;;
		esac
		shift
	done

	[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || die "要在 main 上发布"
	[ -z "$(git status --porcelain --untracked-files=no)" ] || die "有没提交的改动，先提交"
	git fetch --quiet origin main
	[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] ||
		die "main 和 origin/main 不一致：先 git pull --rebase origin main，或者先推送"

	notes=$(unreleased)
	printf '%s\n' "$notes" | grep -q '^- ' || die "CHANGELOG.md 的 \"## Unreleased\" 下没有内容"

	version=$(next)
	say "版本 ${version}，说明："
	printf '%s\n' "$notes" >&2
	if [ "$dry" = 1 ]; then
		say "-n：不改任何东西"
		return
	fi

	awk -v v="$version" '/^## Unreleased$/ { print "## " v; next } { print }' CHANGELOG.md >CHANGELOG.md.new
	mv CHANGELOG.md.new CHANGELOG.md
	msg=$version
	[ -z "$trailer" ] || msg=$(printf '%s\n\n%s' "$version" "$trailer")
	git add CHANGELOG.md
	git commit --quiet -m "$msg"
	git tag "v$version"
	if [ "$push" = 1 ]; then
		if ! git push --atomic origin main "v$version"; then
			git tag -d "v$version" >/dev/null
			git reset --quiet --hard HEAD~1
			die "推送被拒绝，已撤销提交和 tag：git pull --rebase origin main 后再来一次"
		fi
		say "已推送 v${version}；进度见 gh run watch"
	else
		say "已提交并打了 tag v${version}；git push --atomic origin main v${version} 发布"
	fi
}

cmd=${1:-}
[ $# -gt 0 ] && shift
case $cmd in
next) next ;;
tag) tag "$@" ;;
*)
	sed -n '2,/^set -eu/p' "$0" | sed -e '$d' -e 's/^# \{0,1\}//' >&2
	exit 2
	;;
esac
