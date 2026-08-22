{
  description = "stackblaster (sb) — dev environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        sb = pkgs.buildGoModule {
          pname = "sb";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-+m/P4ISOp+l/CDSW8yLbebSeedlSG7uDb4ngYSkApzA=";
          subPackages = [ "cmd/sb" ];
          nativeCheckInputs = [ pkgs.git ];
        };
      in {
        packages = {
          default = sb;
          sb = sb;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            delve
          ];

          shellHook = ''
            echo "sb devshell — $(go version)"
          '';
        };
      });
}
