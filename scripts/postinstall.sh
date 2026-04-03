#!/bin/sh
set -e

# Ensure the binary is executable (belt-and-suspenders — nfpm already sets 0755).
chmod 0755 /usr/local/bin/trangly

# Run preflight checks so the user immediately sees what's ready and what's missing.
# || true: never fail the dpkg install if requirements aren't met yet.
/usr/local/bin/trangly check || true
