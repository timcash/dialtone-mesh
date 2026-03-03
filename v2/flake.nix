{
  description = "mesh v2 build flake";
  inputs = {
    nixpkgs.url = "https://github.com/NixOS/nixpkgs/archive/refs/heads/nixos-24.11.tar.gz";
  };
  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs { inherit system; };
    in
    {
      packages.${system}.default = pkgs.stdenv.mkDerivation {
        name = "mesh_v2";
        src = ./.;
        buildInputs = [ pkgs.libuv ];
        buildPhase = ''
          gcc mesh_v2.c -Os -s -Wall -Wextra -D_FILE_OFFSET_BITS=64 -D_LARGEFILE64_SOURCE 
            -I./libudx/include -I./libudx/build/_deps/github+libuv+libuv-src/include 
            ./libudx/build/libudx.a -luv -lpthread -ldl -lrt -lm -o mesh_v2
        '';
        installPhase = ''
          mkdir -p $out/bin
          cp mesh_v2 $out/bin/
        '';
      };
    };
}
