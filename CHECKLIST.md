# Vice City on goplay — progress

## Phase 1: Playable in the browser
- [x] Clone reVC (miami) source
- [x] Inspect ISO: layout, which files the engine needs, sizes
- [x] Check toolchain (emscripten / cmake / go)
- [x] Build reVC natively on macOS (build_n_run.sh; runs on the 1C ISO data)
- [x] Build reVC for wasm (Emscripten: librw gl3 + GLES + OpenAL) — `./build-wasm.sh`; menu renders at 60 fps in Chromium
- [x] goplay: `-import` finder for VC game data (goplay/cmd/vc; gameimport with streaming Walk, Inno Setup 5.2, Joliet ISO names) — the 1C ISO imports natively in 7 s
- [x] goplay: run an Emscripten engine from play.html (games.js runEm: IndexedDB → MEMFS, saves written back; files > 64 MB stored as Blobs since Chromium rejects bigger IndexedDB values)
- [x] Large-file handling: lazy /games/vc (IndexedDB only), streaming import, ISO dropped from memory after unpacking
- [x] Register `vc` in games.js + deploy.sh
- [x] Local end-to-end test: drop ISO → import → play (headless Chromium: ISO unpacked, imported, new game reaches the intro cutscene; fixes: sfx.RAW case, GAMES not on globalThis in workers, windowed video mode in the browser, SDL needs a window focus event, RenderCB signature mismatch in wasm)
- [x] Box art: fetch-boxes.sh case `vc` (BigBoxCollection GTA3ViceCity, 183x232x26 mm) — run `fetch-boxes.sh vc` when deploying

## Phase 2: Modernization (in reVC source, config.h toggles)
- [~] Framerate-independent physics / 60+ FPS: reVC's FIX_BUGS timer (logical 30 fps frame counter, variable timestep) is on; browser runs at vsync (60 fps headless). Remaining: audit high-FPS physics bugs in play
- [x] Aspect-ratio scaling: ASPECT_RATIO_SCALE on; the browser canvas takes the iframe size (Windowed=1 keeps the ini size)
- [x] Free-orbit camera: FREE_CAM on by default (Cam.cpp, MenuScreensCustom.cpp); vehicle cam no longer recenters after mouse use (Process_FollowCar_SA). Pitch comes from the existing mouse-look
- [x] Analog triggers for accel/brake: sdl2.cpp CapturePad feeds Modern::TriggerL/R, CPad::GetBrake/GetAccelerate take the max of button and trigger
- [x] First-person camera: V cycles near -> far -> first person (Camera.cpp); Process_FollowPedWithMouse puts the cam on PED_HEAD, hides the player model, near clip 0.05
- [x] Over-the-shoulder free aim: while m_bFreeAimActive the follow cam shifts 0.45 m right and 40 % closer
- [x] Upper-body strafe aiming: already in reVC (ProcessAnimGroups via m_fWalkAngle with the mouse cam); nothing to add
- [x] Weapon wheel UI: hold Tab > 200 ms on foot, time scale 0.1, mouse picks one of 10 slots, release applies via m_nSelectedWepSlot (src/extras/modern.cpp)
- [x] Radio wheel UI: hold R > 200 ms in a vehicle, stations + off as text, release sets the car radio
- [x] Drive-by free aim: cars with the mouse cam fire along the camera (CWeapon::FireFromCarFree); bikes/boats keep the stock left/right
- [x] GPS A* route on radar: ThePaths.DoPathSearch to the target marker (or first coord blip) once a second, drawn as a purple polyline in CRadar::DrawBlips
- [x] Cover system: Duck (C) facing a wall within 1.2 m snaps to it; holding aim pops out, releasing re-ducks; moving leaves cover

## Notes
- Native data for testing: ~/Games/GTA-VC (installer payload + reVC gamefiles). `./build_n_run.sh` builds and runs it.
- librw changes (WebGL2 shader decl, ES-only SDL profile) live inside the submodule: fold librw into the repo before pushing.
