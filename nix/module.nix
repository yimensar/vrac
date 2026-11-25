{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.vrac;
  oidc = cfg.oidc;
  args = [
    "${cfg.package}/bin/vrac"
    "serve"
    "-data"
    cfg.dataDir
    "-listen"
    cfg.listenAddress
    "-url"
    cfg.publicURL
    "-timezone"
    cfg.timezone
  ]
  ++ lib.optionals oidc.enable (
    [
      "-oidc-issuer"
      oidc.issuer
      "-oidc-client-id"
      oidc.clientID
      "-oidc-name"
      oidc.name
      "-oidc-allow"
      (lib.concatStringsSep "," oidc.allow)
    ]
    ++ lib.optionals (oidc.clientSecretFile != null) [
      "-oidc-secret-file"
      "%d/oidc-secret"
    ]
  );
in
{
  options.services.vrac = {
    enable = lib.mkEnableOption "Vrac, the memory your AI and you share";
    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "vrac.packages.<system>.default";
      description = "Vrac package to run.";
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      description = "HTTP listen address. Terminate HTTPS at a reverse proxy.";
    };
    publicURL = lib.mkOption {
      type = lib.types.str;
      example = "https://vrac.example.com";
      description = "Public HTTPS URL: the OAuth issuer, the MCP resource and the passkey domain.";
    };
    dataDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/vrac";
      description = "Local directory of the SQLite database; never a network or synchronized filesystem.";
    };
    timezone = lib.mkOption {
      type = lib.types.str;
      default = "Europe/Paris";
      description = "IANA time zone of journal days.";
    };
    oidc = {
      enable = lib.mkEnableOption "sign-in through an OIDC identity provider, such as Pocket ID";
      issuer = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Issuer URL of the identity provider.";
      };
      clientID = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Client ID; register <publicURL>/auth/oidc/callback as its redirect URI.";
      };
      clientSecretFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Runtime path of the client secret, outside the Nix store; null for a public client with PKCE.";
      };
      name = lib.mkOption {
        type = lib.types.str;
        default = "identity provider";
        description = "Provider name shown on the sign-in button.";
      };
      allow = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        description = "Subjects or verified emails allowed to open this Vrac.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion =
          lib.hasPrefix "https://" cfg.publicURL || lib.hasPrefix "http://localhost" cfg.publicURL;
        message = "services.vrac.publicURL must be HTTPS (passkeys and OAuth clients require it).";
      }
      {
        assertion =
          lib.hasPrefix "/" cfg.dataDir
          && cfg.dataDir != "/"
          && builtins.match ".*[[:space:]].*" cfg.dataDir == null;
        message = "services.vrac.dataDir must be an absolute path without whitespace.";
      }
      {
        assertion = !oidc.enable || (oidc.issuer != "" && oidc.clientID != "" && oidc.allow != [ ]);
        message = "services.vrac.oidc needs an issuer, a client ID and at least one allowed identity.";
      }
      {
        assertion = oidc.clientSecretFile == null || !(lib.hasPrefix "/nix/store/" oidc.clientSecretFile);
        message = "services.vrac.oidc.clientSecretFile must stay outside the Nix store.";
      }
    ];

    users.groups.vrac = { };
    users.users.vrac = {
      isSystemUser = true;
      group = "vrac";
      home = cfg.dataDir;
    };
    systemd.tmpfiles.rules = [ "d ${cfg.dataDir} 0700 vrac vrac -" ];
    # Recovery commands (vrac password, vrac passkeys reset) run as the
    # service user against the same directory.
    environment.systemPackages = [ cfg.package ];

    systemd.services.vrac = {
      description = "Vrac";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [ "network-online.target" ];
      environment.ZONEINFO = "${pkgs.tzdata}/share/zoneinfo";
      serviceConfig = {
        User = "vrac";
        Group = "vrac";
        WorkingDirectory = cfg.dataDir;
        ExecStart = lib.escapeShellArgs args;
        LoadCredential = lib.optional (
          oidc.clientSecretFile != null
        ) "oidc-secret:${oidc.clientSecretFile}";
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
        ReadWritePaths = [ cfg.dataDir ];
        TimeoutStopSec = 30;
      };
    };
  };
}
