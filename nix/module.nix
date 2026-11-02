{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.vrac;
  packageSet = self.packages.${pkgs.stdenv.hostPlatform.system};
  args = [
    "${cfg.package}/bin/vrac"
    "serve"
    "--listen"
    cfg.listenAddress
    "--db"
    "${cfg.stateDir}/vrac.sqlite"
    "--token-file"
    "%d/vrac-token"
    "--timezone"
    cfg.timezone
  ]
  ++ lib.optional (!cfg.enableWeb) "--no-web"
  ++ lib.optionals (cfg.oauthIssuer != null) [
    "--oauth-issuer"
    cfg.oauthIssuer
  ]
  ++ lib.optionals (cfg.oauthAudience != null) [
    "--oauth-audience"
    cfg.oauthAudience
  ]
  ++ [
    "--oauth-scope"
    cfg.oauthScope
  ];
in
{
  options.services.vrac = {
    enable = lib.mkEnableOption "the Vrac knowledge service";
    package = lib.mkOption {
      type = lib.types.package;
      default = if cfg.enableWeb then packageSet.vrac else packageSet.vrac-headless;
      defaultText = lib.literalExpression "vrac.packages.<system>.vrac";
      description = "Vrac package to run; the headless package is selected when the web interface is disabled.";
    };
    enableWeb = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Serve the optional web interface alongside the MCP endpoint.";
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      description = "HTTP listen address. Terminate HTTPS at a reverse proxy.";
    };
    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/vrac";
      description = "Local directory for the canonical SQLite database; never use a network or synchronized filesystem.";
    };
    tokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/run/secrets/vrac-token";
      description = "Absolute runtime path to the private browser/generic-MCP token. Never put its contents in a Nix expression or the Nix store.";
    };
    timezone = lib.mkOption {
      type = lib.types.str;
      default = "Europe/Paris";
      description = "IANA timezone used to select the current Journal day.";
    };
    oauthIssuer = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Optional external OAuth/OIDC issuer for ChatGPT and other OAuth MCP clients.";
    };
    oauthAudience = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Expected OAuth token audience, normally the public HTTPS MCP endpoint.";
    };
    oauthScope = lib.mkOption {
      type = lib.types.str;
      default = "vrac";
      description = "OAuth scope required to access Vrac.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion =
          cfg.tokenFile != null
          && lib.hasPrefix "/" cfg.tokenFile
          && !(lib.hasPrefix "/nix/store/" cfg.tokenFile);
        message = "services.vrac.tokenFile must name an absolute runtime secret path outside the Nix store.";
      }
      {
        assertion =
          lib.hasPrefix "/" cfg.stateDir
          && cfg.stateDir != "/"
          && builtins.match ".*[[:space:]].*" cfg.stateDir == null;
        message = "services.vrac.stateDir must be an absolute path without whitespace.";
      }
      {
        assertion = (cfg.oauthIssuer == null) == (cfg.oauthAudience == null);
        message = "Set services.vrac.oauthIssuer and oauthAudience together.";
      }
    ];
    users.groups.vrac = { };
    users.users.vrac = {
      isSystemUser = true;
      group = "vrac";
      home = cfg.stateDir;
    };
    systemd.tmpfiles.rules = [ "d ${cfg.stateDir} 0700 vrac vrac -" ];
    systemd.services.vrac = {
      description = "Vrac knowledge base";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [ "network-online.target" ];
      environment.ZONEINFO = "${pkgs.tzdata}/share/zoneinfo";
      serviceConfig = {
        User = "vrac";
        Group = "vrac";
        WorkingDirectory = cfg.stateDir;
        ExecStart = lib.escapeShellArgs args;
        LoadCredential = lib.optional (cfg.tokenFile != null) "vrac-token:${cfg.tokenFile}";
        Restart = "on-failure";
        RestartSec = 3;
        UMask = "0077";
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictSUIDSGID = true;
        RestrictNamespaces = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        CapabilityBoundingSet = "";
        RestrictAddressFamilies = [
          "AF_UNIX"
          "AF_INET"
          "AF_INET6"
        ];
        ReadWritePaths = [ cfg.stateDir ];
        TimeoutStopSec = 30;
      };
    };
  };
}
