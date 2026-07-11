#!/bin/bash
# iptables-rules.sh — restrict container egress to the LLM provider only.
#
# This script is run inside the dark-research-mcp container
# (as an init script via --cap-add=NET_ADMIN) to drop all
# outbound traffic except to api.minimax.io. The
# rationale: even if a prompt injection or compromised mod
# tries to make the binary POST data to an attacker-
# controlled URL, the iptables rules block it at the
# network layer.
#
# Usage (from docker run, as init):
#   docker run --rm \
#     --cap-add=NET_ADMIN \
#     --init-script=iptables-rules.sh \
#     ...
#
# Or via a wrapper entrypoint that calls this script first.

set -euo pipefail

# Resolve api.minimax.io to its current IP(s) and allow
# outbound to those. Note: this resolves at container
# startup; if the IP changes, restart the container.
LLM_HOST="${DARK_LLM_HOST:-api.minimax.io}"
LLM_IPS=$(getent hosts "$LLM_HOST" | awk '{print $1}' | sort -u)

if [ -z "$LLM_IPS" ]; then
    echo "ERROR: cannot resolve $LLM_HOST" >&2
    exit 1
fi

echo "Allowing egress to: $LLM_HOST ($LLM_IPS)"

# Default policy: drop all outbound.
iptables -P OUTPUT DROP

# Allow loopback (some libraries do local IPC).
iptables -A OUTPUT -o lo -j ACCEPT

# Allow established connections (so the LLM responses
# can flow back).
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT

# Allow DNS resolution (so the binary can resolve the
# LLM host).
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT

# Allow TCP to the LLM host IPs only.
for ip in $LLM_IPS; do
    iptables -A OUTPUT -p tcp -d "$ip" --dport 443 -j ACCEPT
    iptables -A OUTPUT -p tcp -d "$ip" --dport 80 -j ACCEPT
done

# Log dropped traffic for audit.
iptables -A OUTPUT -m limit --limit 10/min -j LOG --log-prefix "drk-dropped: " --log-level 4

echo "Egress restricted. Only $LLM_HOST allowed."

# Now exec the actual binary. The orchestrator is
# expected to invoke this script via docker exec or as
# the entrypoint.
exec "$@"
