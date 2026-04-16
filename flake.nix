{
  description = "Browser-based markdown review tool with inline commenting";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      version = "0.9.1";
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in rec {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          crit = pkgs.buildGo126Module {
            pname = "crit";
            inherit version;
            src = self;
            vendorHash = "sha256-n2yA86hAhSipIhQw9HSKubCVT4RrPdau+/Ve7ebrevc=";
            # Tests run in dedicated CI jobs (test + e2e); the Nix sandbox's
            # /build TMPDIR cleanup races with the debounced review file writer.
            doCheck = false;
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
            meta = with nixpkgs.lib; {
              description = "Browser-based markdown review tool with inline commenting";
              homepage = "https://github.com/tomasz-tomczyk/crit";
              license = licenses.mit;
              mainProgram = "crit";
            };
          };
        in {
          inherit crit;
          default = crit;
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${packages.${system}.default}/bin/crit";
        };
      });

      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_26
              gopls
              golangci-lint
              git
            ];
          };
        });
    };
}
