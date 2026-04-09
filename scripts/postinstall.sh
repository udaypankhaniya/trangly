#!/bin/sh
set -e

# $1 is "configure" on fresh install or upgrade; other values (abort-upgrade,
# abort-remove, etc.) mean dpkg is rolling back — do nothing.
case "$1" in
    configure)
        # Ensure the binary is executable (belt-and-suspenders).
        chmod 0755 /usr/local/bin/trangly

        # Run preflight checks so the user sees what's ready and what's missing.
        # Never fail the dpkg install if requirements aren't met yet.
        /usr/local/bin/trangly check || true
        ;;
    abort-upgrade|abort-remove|abort-deconfigure)
        ;;
    *)
        ;;
esac

exit 0
