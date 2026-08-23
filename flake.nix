{
  description = "Multica development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forEachSystem = nixpkgs.lib.genAttrs systems;
    in {
      devShells = forEachSystem (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell {
            packages = [
              pkgs.nodejs_22
              pkgs.pnpm_10
              pkgs.go_1_26
            ];

            shellHook = ''
              export NIX_MULTICA_DEV_SHELL=1
              echo "Multica dev shell: Node $(node --version), pnpm $(pnpm --version), Go $(go version | awk '{print $3}')"
            '';
          };
        });
    };
}
