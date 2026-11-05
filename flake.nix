{
  description = "Vrac: the memory your AI and you share";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = lib.genAttrs systems;
      version = lib.removeSuffix "\n" (builtins.readFile ./VERSION);
      ignored = [
        "node_modules"
        "dist"
        ".vite"
        "bin"
        "data"
      ];
      clean =
        src:
        lib.cleanSourceWith {
          inherit src;
          filter = path: type: lib.cleanSourceFilter path type && !(lib.elem (baseNameOf path) ignored);
        };
      mkWeb =
        pkgs:
        pkgs.buildNpmPackage {
          pname = "vrac-web";
          inherit version;
          src = clean ./web;
          npmDepsHash = "sha256-0UmHgthS5hGXfKg+0sQ9JD5seCLcnFio/jhmKxiiHC0=";
          installPhase = ''
            runHook preInstall
            cp -r dist $out
            runHook postInstall
          '';
        };
      mkVrac =
        pkgs:
        pkgs.buildGoModule {
          pname = "vrac";
          inherit version;
          src = clean ./.;
          vendorHash = "sha256-2H6vPT96FfWhauIAPZTrlsKe8BcNOZMGjfK1IUVopq0=";
          subPackages = [ "cmd/vrac" ];
          env.CGO_ENABLED = "1";
          tags = [ "sqlite_fts5" ];
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          preBuild = ''
            cp -r ${mkWeb pkgs}/. web/dist/
          '';
          meta = {
            description = "The memory your AI and you share";
            license = lib.licenses.agpl3Only;
            mainProgram = "vrac";
            platforms = lib.platforms.unix;
          };
        };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = mkVrac pkgs;
          web = mkWeb pkgs;
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = lib.getExe self.packages.${system}.default;
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              nodejs_24
              sqlite
              jq
              curl
              nixfmt
              stdenv.cc
            ];
            CGO_ENABLED = "1";
            GOFLAGS = "-tags=sqlite_fts5";
            shellHook = ''
              export GOTOOLCHAIN=local
            '';
          };
        }
      );

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt);
    };
}
