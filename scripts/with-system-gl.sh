#!/usr/bin/env sh
# Run a command against the system OpenGL stack.
#
# Nix-based tooling (nixGL wrappers, a Nix-installed terminal, ...) exports
# driver-discovery variables pointing at /nix/store builds linked against a
# newer glibc than the system one. Two independent lookups break because of it:
#
#   LD_LIBRARY_PATH                 -> libGL.so: version `GLIBC_2.38' not found
#   __EGL_VENDOR_LIBRARY_FILENAMES  -> glfw: EGL: Failed to get EGL display
#
# The second is the subtle one: libglvnd reads it on its own, so clearing
# LD_LIBRARY_PATH alone is not enough. Ebitengine goes through EGL on Linux,
# which is why the EGL vendor file decides whether the window opens at all.
#
# Harmless on machines without Nix: unsetting an unset variable is a no-op.
#
# Usage: scripts/with-system-gl.sh <command> [args...]

set -eu

if [ "$#" -eq 0 ]; then
    echo "usage: $0 <command> [args...]" >&2
    exit 2
fi

exec env \
    -u LD_LIBRARY_PATH \
    -u __EGL_VENDOR_LIBRARY_FILENAMES \
    -u LIBGL_DRIVERS_PATH \
    -u GBM_BACKENDS_PATH \
    -u LIBVA_DRIVERS_PATH \
    "$@"
