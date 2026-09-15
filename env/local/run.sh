#!/bin/sh
# Run a client in netns $NS_A against memsrv in $NS_B, per env.conf: bring the
# env up, start memsrv, wait until it listens, run the command in $NS_A
# ($RUNS times, default 1), then stop memsrv and tear the env down, also when
# something fails. Needs root and built binaries; `make docker-cli0` does both.
#
#   env/local/run.sh out/linux_arm64/cli0 -env local
set -eu
[ $# -gt 0 ] || { echo "usage: $0 <client> [args...]" >&2; exit 2; }
dir=$(dirname "$0")
. "$dir/env.conf"
out=${OUT:-out/$(go env GOOS)_$(go env GOARCH)}

"$dir/up.sh"
ip netns exec "$NS_B" "$out/memsrv" -env local &
srv=$!
trap 'kill "$srv" 2>/dev/null || true; wait "$srv" 2>/dev/null || true; "$dir/down.sh"' EXIT

# wait for memsrv on the address env.conf gives it: fail fast if it died, give up after 10 s
timeout 10 sh -c "until ip netns exec $NS_B ss -ltn | grep -q '$ADDR_B:$PORT'; do
	kill -0 $srv 2>/dev/null || exit 1
	sleep 0.05
done" || { echo "memsrv not listening on $ADDR_B:$PORT" >&2; exit 1; }

i=0
while [ "$i" -lt "${RUNS:-1}" ]; do
	i=$((i + 1))
	[ "${RUNS:-1}" -gt 1 ] && echo "--- run $i ---"
	ip netns exec "$NS_A" "$@"
done
