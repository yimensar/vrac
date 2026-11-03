{
  description = "Vrac: a journal-centered knowledge base for people and AI";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = lib.genAttrs systems;
      version = lib.removeSuffix "\n" (builtins.readFile ./VERSION);
      source = lib.cleanSourceWith {
        src = ./.;
        filter =
          path: type:
          let
            root = toString ./.;
            relative = lib.removePrefix (root + "/") (toString path);
            top = builtins.head (lib.splitString "/" relative);
            name = baseNameOf path;
          in
          (
            toString path == root
            || lib.elem top [
              "cmd"
              "internal"
              "web"
              "go.mod"
              "go.sum"
              "VERSION"
              "LICENSE"
            ]
          )
          && lib.cleanSourceFilter path type
          && !(lib.elem name [
            "node_modules"
            "dist"
            ".vite"
          ]);
      };
      mkFrontend =
        pkgs:
        pkgs.buildNpmPackage {
          pname = "vrac-web";
          inherit version;
          src = lib.cleanSourceWith {
            src = ./web;
            filter =
              path: type:
              lib.cleanSourceFilter path type
              && !(lib.elem (baseNameOf path) [
                "node_modules"
                "dist"
                ".vite"
              ]);
          };
          npmDepsHash = "sha256-0MeyWgueBva6odDpOycE0bplk3p/uVyjVRdBF+4tNQk=";
          npmBuildScript = "build";
          installPhase = ''
            runHook preInstall
            mkdir -p $out
            cp -r dist $out/dist
            runHook postInstall
          '';
        };
      mkVrac =
        pkgs: withWeb:
        let
          buildTags = [ "sqlite_fts5" ] ++ lib.optional (!withWeb) "headless";
        in
        pkgs.buildGoModule {
          pname = if withWeb then "vrac" else "vrac-headless";
          inherit version;
          src = source;
          vendorHash = "sha256-IojoPcbp1B6ZXwHoqka+ZwHHFDlvXFgzDh5720Ka3TU=";
          subPackages = [ "cmd/vrac" ];
          env.CGO_ENABLED = "1";
          tags = buildTags;
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          preBuild = lib.optionalString withWeb ''
            mkdir -p web/dist
            cp -r ${mkFrontend pkgs}/dist/. web/dist/
          '';
          checkPhase = ''
            runHook preCheck
            go test -tags ${lib.escapeShellArg (lib.concatStringsSep "," buildTags)} -timeout=5m ./...
            runHook postCheck
          '';
          meta = {
            description = "Journal-centered knowledge base with web and MCP interfaces";
            homepage = "https://github.com/yan-imensar/vrac";
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
          vrac = mkVrac pkgs true;
          headless = mkVrac pkgs false;
        in
        {
          inherit vrac;
          default = vrac;
          vrac-headless = headless;
          web = mkFrontend pkgs;
        }
        // lib.optionalAttrs pkgs.stdenv.isLinux {
          docker = pkgs.dockerTools.buildLayeredImage {
            name = "vrac";
            tag = version;
            created = "1970-01-01T00:00:01Z";
            contents = [
              vrac
              pkgs.cacert
              pkgs.tzdata
            ];
            extraCommands = ''
              mkdir -p data etc tmp
              chmod 1777 tmp
              printf 'vrac:x:10001:10001:Vrac:/data:/sbin/nologin\n' > etc/passwd
              printf 'vrac:x:10001:\n' > etc/group
            '';
            fakeRootCommands = ''
              chown 10001:10001 data
              chmod 0700 data
            '';
            config = {
              User = "10001:10001";
              Entrypoint = [ "${vrac}/bin/vrac" ];
              Cmd = [ "serve" ];
              WorkingDir = "/data";
              Env = [
                "VRAC_LISTEN=0.0.0.0:8080"
                "VRAC_DB=/data/vrac.sqlite"
                "VRAC_TOKEN_FILE=/run/secrets/vrac-token"
                "VRAC_TIMEZONE=Europe/Paris"
                "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
                "ZONEINFO=${pkgs.tzdata}/share/zoneinfo"
              ];
              ExposedPorts."8080/tcp" = { };
              Volumes."/data" = { };
              Labels = {
                "org.opencontainers.image.title" = "Vrac";
                "org.opencontainers.image.version" = version;
                "org.opencontainers.image.source" = "https://github.com/yan-imensar/vrac";
                "org.opencontainers.image.licenses" = "AGPL-3.0-only";
              };
            };
          };
        }
      );

      nixosModules.default = import ./nix/module.nix { inherit self; };
      nixosModules.vrac = self.nixosModules.default;

      checks = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          moduleConfig = lib.nixosSystem {
            inherit system;
            modules = [
              self.nixosModules.default
              {
                system.stateVersion = "26.05";
                services.vrac = {
                  enable = true;
                  enableWeb = false;
                  tokenFile = "/run/secrets/vrac-token";
                  oauthIssuer = "https://identity.example.test";
                  oauthAudience = "https://knowledge.example.test/mcp";
                };
              }
            ];
          };
          service = moduleConfig.config.systemd.services.vrac.serviceConfig;
        in
        {
          inherit (self.packages.${system}) vrac;
          headless = self.packages.${system}.vrac-headless;
        }
        // lib.optionalAttrs pkgs.stdenv.isLinux {
          nixos-module =
            assert service.User == "vrac";
            assert service.ProtectSystem == "strict";
            assert lib.hasInfix "--no-web" service.ExecStart;
            assert lib.hasInfix "--oauth-issuer" service.ExecStart;
            pkgs.writeText "vrac-nixos-module-check" (
              builtins.toJSON {
                inherit (service)
                  User
                  Group
                  ExecStart
                  LoadCredential
                  ReadWritePaths
                  ;
              }
            );
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              nodejs_24
              sqlite
              python3
              jq
              curl
              git
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
