#!/bin/sh
# build-wasm.sh: build reVC for the browser (wasm/CMakeLists.txt) into
# build-wasm/reVC.{js,wasm}. Needs emscripten, cmake and ninja (brew).
set -e
cd "$(dirname "$0")"
git submodule update --init --depth 1 vendor/librw
emcmake cmake -S wasm -B build-wasm -G Ninja -DCMAKE_BUILD_TYPE=Release >/dev/null
cmake --build build-wasm
ls -la build-wasm/reVC.js build-wasm/reVC.wasm
