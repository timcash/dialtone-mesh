# mesh v2 Makefile

CC = cc
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# Defaults
OUT = dialtone_mesh_v2
LIBUDX_DIR = libudx
LIBUDX_BUILD = $(LIBUDX_DIR)/build
UDX_LIB = $(shell find $(LIBUDX_BUILD) -name libudx.a | head -n1)
UV_LIB = $(shell find $(LIBUDX_BUILD) -name libuv.a | head -n1)

CFLAGS = -Os -s -Wall -Wextra -D_FILE_OFFSET_BITS=64 -D_LARGEFILE64_SOURCE
INCLUDES = -I$(LIBUDX_DIR)/include -I$(LIBUDX_BUILD)/_deps/github+libuv+libuv-src/include

ifeq ($(UNAME_S),Darwin)
	CC = clang
	CFLAGS += -mmacosx-version-min=12.0
	LIBS = -lpthread -ldl -lm -framework CoreFoundation -framework Security -framework IOKit
	OUT_BIN = $(OUT)_$(UNAME_M)
else
	# Linux build
	CC = gcc
	LIBS = -lpthread -ldl -lrt -lm
	OUT_BIN = $(OUT)_$(UNAME_M)
endif

.PHONY: all clean libudx

all: $(OUT_BIN)

libudx:
	cd $(LIBUDX_DIR) && npm install
	cd $(LIBUDX_DIR) && CC=$(CC) npx bare-make generate -D CMAKE_C_COMPILER=$(CC) -D CMAKE_C_FLAGS="-Wno-error -D_FILE_OFFSET_BITS=32"
	cd $(LIBUDX_DIR) && npx bare-make build

$(OUT_BIN): mesh_v2.c libudx
	$(CC) mesh_v2.c $(CFLAGS) $(INCLUDES) $(UDX_LIB) $(UV_LIB) $(LIBS) -o $(OUT_BIN)

deploy-rover: $(OUT)_arm64
	scp $(OUT)_arm64 rover:~/dialtone/

clean:
	rm -f $(OUT)_*
	rm -rf $(LIBUDX_BUILD)
