#!/bin/sh
# Bring up the `local` env: netns $NS_A <-veth-> $NS_B, per env.conf.
# Idempotent: re-running converges to the same state. MTU=<n> overrides env.conf.
# Needs CAP_NET_ADMIN: run inside the privileged dev container (make dev).
set -eu

# Allow MTU ovverride from cmd line
mtu=${MTU:-}

# Load env values
. "$(dirname "$0")/env.conf"
MTU=${mtu:-$MTU}

# Creating network Namespaces
for ns in "$NS_A" "$NS_B"; do
	[ -e "/run/netns/$ns" ] || ip netns add "$ns"
	ip -n "$ns" link set lo up
done

# create both ends directly inside their namespaces; the pair comes and goes as one
if ! ip -n "$NS_A" link show "$IFACE_A" >/dev/null 2>&1; then
	ip link add "$IFACE_A" netns "$NS_A" type veth peer name "$IFACE_B" netns "$NS_B"
fi

ip -n "$NS_A" link set "$IFACE_A" mtu "$MTU" up
ip -n "$NS_B" link set "$IFACE_B" mtu "$MTU" up
ip -n "$NS_A" addr replace "$ADDR_A/$PREFIX" dev "$IFACE_A"
ip -n "$NS_B" addr replace "$ADDR_B/$PREFIX" dev "$IFACE_B"

ip netns exec "$NS_A" ping -c 1 -W 1 -q "$ADDR_B" >/dev/null
echo "local env up: $NS_A $ADDR_A <-> $NS_B $ADDR_B (mtu $MTU)"
