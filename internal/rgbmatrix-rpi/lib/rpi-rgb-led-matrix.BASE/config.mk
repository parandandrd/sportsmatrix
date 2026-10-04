# Choose LTO options that works with the chosen compiler. Also, the
# `ar` link archiver needs to be chosen as they are very specific to that
# version.
COMPILER_VERSION := $(shell $(CXX) --version 2>/dev/null)
ifneq (,$(findstring clang,$(COMPILER_VERSION)))
  LTO_FLAGS=-flto=thin
  AR=llvm-ar
else
  LTO_FLAGS=-flto=2
  AR=gcc-ar
endif

# sportsmatrix: no LTO. The objects go into librgbmatrix.a, which cgo links
# into the Go binary; as plain object code, as they always were here.
LTO_FLAGS=

# When cross-compiling for aarch64 (e.g. using aarch64-linux-gnu-g++),
# the compiler does not accept '-march=native' or '-mtune=native'. Detect
# that situation and avoid those flags. For native builds keep the flags.
ifneq (,$(findstring aarch64-linux-gnu,$(CXX)))
  CPU_ARCH_FLAGS=
else
  CPU_ARCH_FLAGS=-march=native -mtune=native
endif

# sportsmatrix: never -march=native. The release .deb is built on a GitHub
# arm64 runner, a far newer core than the Cortex-A53 in the Pi 3 and the
# Zero 2 W; code tuned for it can die there with SIGILL. Plain armv8-a, the
# compiler default, runs on both.
CPU_ARCH_FLAGS=
