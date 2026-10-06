#!/bin/sh
set -eu

# This wrapper is mounted read-only and invoked only while the host flock for
# this application's private core directory is held. The previous bwrap PID
# namespace must have exited before that lock can be acquired. Its Chromium
# processes therefore cannot still own these singleton links after a crash.
# The standalone/desktop browser's conservative lock handling is unchanged.
for profile in /data/profiles/*; do
    [ -d "$profile" ] || continue
    [ ! -L "$profile" ] || exit 1
    rm -f -- "$profile/SingletonLock" "$profile/SingletonSocket" "$profile/SingletonCookie" "$profile/DevToolsActivePort"
done

exec /opt/svolo-core serve --data /data --listen 127.0.0.1:0 \
    --headless --chromium /opt/chrome/chrome "$@"
