# reVC for the browser: Emscripten, librw GL3 on WebGL2 (GLES3) through SDL2, OpenAL.
# Build with ../build-wasm.sh. Separate from the main CMakeLists on purpose:
# that one hunts for system libraries; here every dependency is an Emscripten port.
cmake_minimum_required(VERSION 3.14)
project(reVCwasm C CXX)
set(R ${CMAKE_CURRENT_SOURCE_DIR}/..)

file(GLOB_RECURSE SRC ${R}/src/*.cpp ${R}/src/*.c)
list(FILTER SRC EXCLUDE REGEX "/skel/(win|glfw|android)/|/extras/GitSHA1.cpp$")
execute_process(COMMAND git -C ${R} rev-parse HEAD OUTPUT_VARIABLE GIT_SHA1 OUTPUT_STRIP_TRAILING_WHITESPACE)
configure_file(${R}/src/extras/GitSHA1.cpp.in ${CMAKE_BINARY_DIR}/GitSHA1.cpp @ONLY)
list(APPEND SRC ${CMAKE_BINARY_DIR}/GitSHA1.cpp)
file(GLOB RW ${R}/vendor/librw/src/*.cpp ${R}/vendor/librw/src/gl/*.cpp ${R}/vendor/librw/src/gl/glad/glad.c ${R}/vendor/librw/src/lodepng/lodepng.cpp
	${R}/vendor/librw/src/d3d/*.cpp ${R}/vendor/librw/src/ps2/*.cpp)

file(GLOB_RECURSE HDRS ${R}/src/*.h)
set(INC "")
foreach(h ${HDRS})
	get_filename_component(d ${h} DIRECTORY)
	list(APPEND INC ${d})
endforeach()
list(REMOVE_DUPLICATES INC)

add_executable(reVC ${SRC} ${RW})
target_include_directories(reVC PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/include ${INC} ${R}/vendor/librw ${R}/vendor/librw/src)
target_compile_definitions(reVC PRIVATE LIBRW RW_GL3 LIBRW_SDL2 AUDIO_OAL CMAKE_BUILD USE_OUR_VERSIONING NDEBUG LODEPNG_NO_COMPILE_CPP)
set(PORTS -sUSE_SDL=2 -sUSE_MPG123=1)
target_compile_options(reVC PRIVATE ${PORTS} -O2 -w -fno-strict-aliasing)
set_target_properties(reVC PROPERTIES CXX_STANDARD 14 C_STANDARD 11 SUFFIX .js)
target_link_options(reVC PRIVATE ${PORTS} -O2 -lopenal
	-sMIN_WEBGL_VERSION=2 -sMAX_WEBGL_VERSION=2 -sFULL_ES3=1 -sGL_ENABLE_GET_PROC_ADDRESS=1
	-sALLOW_MEMORY_GROWTH=1 -sMAXIMUM_MEMORY=4GB -sINITIAL_MEMORY=256MB -sSTACK_SIZE=8MB
	-sMODULARIZE=1 -sEXPORT_NAME=reVC -sINVOKE_RUN=0 -sEXIT_RUNTIME=1 -sFORCE_FILESYSTEM=1
	-sENVIRONMENT=web -sEXPORTED_RUNTIME_METHODS=FS,callMain -sASSERTIONS=1 --profiling-funcs)
