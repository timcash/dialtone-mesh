{ pkgs ? import <nixpkgs> {} }:
let
  staticPkgs = pkgs.pkgsStatic;
in
staticPkgs.stdenv.mkDerivation {
  name = "dialtone-mesh-v2-static";
  src = ./.;
  buildInputs = [ staticPkgs.libuv ];
  nativeBuildInputs = [ staticPkgs.gcc ];
  buildPhase = ''
    $CC mesh_v2.c -Os -s -Wall -Wextra -luv -lpthread -o dialtone_mesh_v2_static
  '';
  installPhase = ''
    mkdir -p $out/bin
    cp dialtone_mesh_v2_static $out/bin/
  '';
}
