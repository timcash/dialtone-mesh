# mesh v2 build
#
# Upstream libudx + bare-make flow:
#   npm install
#   bare-make generate
#   bare-make build

CC := clang
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

OUT := dialtone_mesh_v2_$(UNAME_M)
REPO_ROOT := $(abspath $(CURDIR)/../../../..)
BIN_DIR := $(REPO_ROOT)/bin
OUT_BIN := $(BIN_DIR)/$(OUT)
EX_CLIENT := $(BIN_DIR)/dialtone_libudx_client_$(UNAME_M)
EX_SERVER := $(BIN_DIR)/dialtone_libudx_server_$(UNAME_M)
EX_UDXPERF := $(BIN_DIR)/dialtone_libudx_udxperf_$(UNAME_M)
LIBUDX_DIR := libudx
LIBUDX_BUILD := $(LIBUDX_DIR)/build
UDX_LIB := $(LIBUDX_BUILD)/libudx.a
UV_LIB := $(LIBUDX_BUILD)/_deps/github+libuv+libuv-build/libuv.a

CFLAGS := -Os -s -Wall -Wextra
INCLUDES := -I$(LIBUDX_DIR)/include -I$(LIBUDX_BUILD)/_deps/github+libuv+libuv-src/include

ifeq ($(UNAME_S),Darwin)
  LIBS := -lpthread -ldl -lm -framework CoreFoundation -framework Security -framework IOKit
else
  LIBS := -lpthread -ldl -lrt -lm
endif

.PHONY: all deps generate build-libudx examples mesh clean help

all: mesh

deps:
	cd $(LIBUDX_DIR) && npm install

generate: deps
	cd $(LIBUDX_DIR) && npx bare-make generate

build-libudx: generate
	cd $(LIBUDX_DIR) && npx bare-make build

# Examples are built by `bare-make build` (client/server/udxperf).
examples: build-libudx
	mkdir -p $(BIN_DIR)
	cp $(LIBUDX_BUILD)/examples/client $(EX_CLIENT)
	cp $(LIBUDX_BUILD)/examples/server $(EX_SERVER)
	cp $(LIBUDX_BUILD)/examples/udxperf $(EX_UDXPERF)
	@echo "Built examples:"
	@echo "  $(EX_CLIENT)"
	@echo "  $(EX_SERVER)"
	@echo "  $(EX_UDXPERF)"

mesh: build-libudx
	mkdir -p $(BIN_DIR)
	$(CC) mesh_v2.c $(CFLAGS) $(INCLUDES) $(UDX_LIB) $(UV_LIB) $(LIBS) -o $(OUT_BIN)
	@echo "Built: $(OUT_BIN)"

help:
	@echo "Targets:"
	@echo "  make deps          - npm install in libudx"
	@echo "  make generate      - bare-make generate in libudx"
	@echo "  make build-libudx  - bare-make build in libudx"
	@echo "  make examples      - build and print example binary paths"
	@echo "  make mesh          - build mesh_v2 binary"
	@echo "  make all           - same as make mesh"
	@echo ""
	@echo "Use with Dialtone nix shell on WSL or gold:"
	@echo "  ~/dialtone/dialtone.sh make -C ~/dialtone/src/mods/mesh/v2 mesh"

clean:
	rm -f $(OUT_BIN) $(EX_CLIENT) $(EX_SERVER) $(EX_UDXPERF)
	rm -rf $(LIBUDX_BUILD)
