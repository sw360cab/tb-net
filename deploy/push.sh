#!/bin/sh
# Build ultramem for a Linux host, copy it to /opt/ultramem and, for role b,
# (re)start memsrv.service (CLAUDE.md Day 4).
#
#   deploy/push.sh <host> <a|b>
#
# Binaries are stamped with the commit so ultrabench can attribute results
# outside the repository; a dirty tree stamps <sha>-dirty, which ultrabench
# refuses to measure with. GOARCH defaults to amd64 (screwdriver).
set -eu
usage() { echo "usage: $0 <host> <a|b>" >&2; exit 2; }
[ $# -eq 2 ] || usage
host=$1 role=$2
case $role in a | b) ;; *) usage ;; esac
cd "$(dirname "$0")/.."

commit=$(git rev-parse --short HEAD)
[ -z "$(git status --porcelain -- . ':(exclude)bench/results')" ] || commit=$commit-dirty

out=out/push
rm -rf "$out"
mkdir -p "$out/bin"
for b in ultrabench cli0 memsrv; do
	GOOS=linux GOARCH=${GOARCH:-amd64} CGO_ENABLED=0 go build -trimpath \
		-ldflags "-X main.commit=$commit" -o "$out/bin/$b" "./cmd/$b"
done
cp -R env "$out/"

echo "push: $commit -> $host:/opt/ultramem (role $role)"
rsync -a --delete "$out/" "$host:ultramem/"
case $role in
a) ssh -t "$host" 'sudo rsync -a --delete ultramem/ /opt/ultramem/' ;;
b)
	rsync -a deploy/memsrv.service "$host:ultramem/"
	ssh -t "$host" 'sudo rsync -a --delete ultramem/ /opt/ultramem/ &&
		sudo install -m 0644 /opt/ultramem/memsrv.service /etc/systemd/system/ &&
		sudo systemctl daemon-reload && sudo systemctl enable memsrv && sudo systemctl restart memsrv &&
		systemctl --no-pager --lines=5 status memsrv'
	;;
esac
