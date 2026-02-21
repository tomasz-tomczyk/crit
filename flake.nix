{
  description = "Browser-based markdown review tool with inline commenting";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      version = "0.3.3";
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in {
      packages = forAllSystems (system: {
        default = nixpkgs.legacyPackages.${system}.buildGoModule {
          pname = "crit";
          inherit version;
          src = self;
          vendorHash = null;
          ldflags = [ "-s" "-w" "-X main.version=${version}" ];
          meta = with nixpkgs.lib; {
            description = "Browser-based markdown review tool with inline commenting";
            homepage = "https://github.com/tomasz-tomczyk/crit";
            license = licenses.mit;
            mainProgram = "crit";
          };
        };
      });
    };
}
