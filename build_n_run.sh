#!/bin/sh
# build_n_run.sh [SRC]: build the native macOS reVC (librw GL3 + GLFW, OpenAL) into
# build-native/ and run it. The game data is imported once from SRC (default
# ~/Downloads/GTAVC: folders, zips, ISO/BIN/CUE discs, GOG or InstallShield installers)
# into ~/Games/GTAVC by the goplay import tool; delete that folder to re-import.
# Needs: brew install cmake glfw openal-soft mpg123 go
set -e
cd "$(dirname "$0")"
SRC=${1:-$HOME/Downloads/GTAVC}
GAMEDIR=$HOME/Games/GTAVC
cmake -S . -B build-native -DCMAKE_BUILD_TYPE=RelWithDebInfo \
	-DLIBRW_PLATFORM=GL3 -DLIBRW_GL3_GFXLIB=GLFW -DREVC_AUDIO=OAL \
	-DREVC_WITH_OPUS=OFF -DREVC_WITH_LIBSNDFILE=OFF -DLIBRW_TOOLS=OFF -DLIBRW_INSTALL=OFF -DREVC_INSTALL=OFF >/dev/null
cmake --build build-native -j"$(sysctl -n hw.ncpu)"
if [ ! -f "$GAMEDIR/models/gta3.img" ]; then
	rm -rf goplay/cmd/vc/gamefiles && cp -R gamefiles goplay/cmd/vc/gamefiles
	(cd goplay/cmd/vc && go run . -import "$SRC" -data "$GAMEDIR")
fi
cp -R gamefiles/. "$GAMEDIR/" # reVC's own files (controller db, custom models) may have changed
cd "$GAMEDIR" && exec "$OLDPWD/build-native/src/reVC.app/Contents/MacOS/reVC"
