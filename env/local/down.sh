#!/bin/sh
# Tear down the `local` env. Idempotent. Deleting a netns destroys the veth end
# inside it, which removes the whole pair. Stop memsrv/pager first: a process
# still running in a netns keeps it alive after the delete.
set -eu

. "$(dirname "$0")/env.conf"

for ns in "$NS_A" "$NS_B"; do
	if [ -e "/run/netns/$ns" ]; then
		ip netns del "$ns"
	fi
done
echo "local env down"
