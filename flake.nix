{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
  };

  outputs = {nixpkgs, ...}: let
    forAllSystems = function:
      nixpkgs.lib.genAttrs nixpkgs.lib.systems.flakeExposed (system:
        function (import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true;
            android_sdk.accept_license = true;
          };
        }));
  in {
    formatter = forAllSystems (pkgs: pkgs.alejandra);
    devShells = forAllSystems (pkgs: let
      androidSdk = pkgs.androidenv.composeAndroidPackages {
        platformVersions = ["36"];
        buildToolsVersions = ["35.0.0" "36.0.0"];
        cmakeVersions = ["3.22.1"];
        includeNDK = true;
        ndkVersions = ["27.1.12297006"];
      };
    in {
      default = pkgs.mkShell {
        packages = with pkgs; [
          androidSdk.androidsdk
          corepack
          docker-client
          git
          go_1_26
          gnumake
          jdk17
          nodejs_22
          sqlc
        ];

        ANDROID_HOME = "${androidSdk.androidsdk}/libexec/android-sdk";
        ANDROID_SDK_ROOT = "${androidSdk.androidsdk}/libexec/android-sdk";
        JAVA_HOME = pkgs.jdk17.home;
      };
    });
  };
}
