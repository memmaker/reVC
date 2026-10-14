#!/bin/sh
# build_n_run.sh [GAMEDIR]: build the native macOS reVC (librw GL3 + GLFW,
# OpenAL) into build-native/ and run it on a Vice City install (default
# ~/Games/GTA-VC; reVC's gamefiles/ are laid over it first).
# Needs: brew install cmake glfw openal-soft mpg123
set -e
cd "$(dirname "$0")"
GAMEDIR=${1:-$HOME/Games/GTA-VC}
git submodule update --init --depth 1 vendor/librw
cmake -S . -B build-native -DCMAKE_BUILD_TYPE=RelWithDebInfo \
	-DLIBRW_PLATFORM=GL3 -DLIBRW_GL3_GFXLIB=GLFW -DREVC_AUDIO=OAL \
	-DREVC_WITH_OPUS=OFF -DREVC_WITH_LIBSNDFILE=OFF -DLIBRW_TOOLS=OFF -DLIBRW_INSTALL=OFF -DREVC_INSTALL=OFF >/dev/null
cmake --build build-native -j"$(sysctl -n hw.ncpu)"
[ -f "$GAMEDIR/models/gta3.img" ] || { echo "no Vice City in $GAMEDIR (models/gta3.img missing)" >&2; exit 1; }
cp -R gamefiles/. "$GAMEDIR/"
cd "$GAMEDIR" && exec "$OLDPWD/build-native/src/reVC.app/Contents/MacOS/reVC"
