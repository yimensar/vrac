package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/yan-imensar/vrac/internal/engine"
	"github.com/yan-imensar/vrac/internal/server"
	"github.com/yan-imensar/vrac/web"
)

var version = "0.2.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vrac:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command == "version" || command == "--version" {
		fmt.Println(version)
		return nil
	}
	if command == "help" || command == "--help" {
		fmt.Println("Vrac — a journal-centered knowledge base for people and AI\n\nCommands: serve, check, backup, export, import, reindex, version\nRun vrac <command> --help for options.")
		return nil
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	dbPath := flags.String("db", env("VRAC_DB", "data/vrac.sqlite"), "SQLite database on local storage")
	if command == "serve" {
		listen := flags.String("listen", env("VRAC_LISTEN", "127.0.0.1:8080"), "HTTP listen address")
		tokenFile := flags.String("token-file", os.Getenv("VRAC_TOKEN_FILE"), "File containing an access token (at least 32 characters)")
		timezone := flags.String("timezone", env("VRAC_TIMEZONE", "Europe/Paris"), "IANA timezone for journal dates")
		issuer := flags.String("oauth-issuer", os.Getenv("VRAC_OAUTH_ISSUER"), "External OIDC issuer for ChatGPT OAuth")
		audience := flags.String("oauth-audience", os.Getenv("VRAC_OAUTH_AUDIENCE"), "Canonical public HTTPS MCP URL expected as token audience")
		scope := flags.String("oauth-scope", env("VRAC_OAUTH_SCOPE", "vrac"), "Required scope in external OAuth tokens")
		webClient := flags.String("web-oidc-client-id", os.Getenv("VRAC_WEB_OIDC_CLIENT_ID"), "Public PKCE client ID for browser sign-in with the configured OIDC provider")
		webProvider := flags.String("web-oidc-provider-name", env("VRAC_WEB_OIDC_PROVIDER_NAME", "Single sign-on"), "Provider name displayed on the browser sign-in button")
		noWebDefault, err := strconv.ParseBool(env("VRAC_NO_WEB", "false"))
		if err != nil {
			return errors.New("VRAC_NO_WEB must be true or false")
		}
		noWeb := flags.Bool("no-web", noWebDefault, "Disable the optional web UI")
		dev := flags.Bool("dev", false, "Unauthenticated loopback-only development mode")
		if err := flags.Parse(args); err != nil {
			return flagError(err)
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		if *dev && !loopbackAddress(*listen) {
			return errors.New("--dev requires a loopback listen address")
		}
		var token string
		if *tokenFile != "" {
			data, err := os.ReadFile(*tokenFile)
			if err != nil {
				return fmt.Errorf("read token file: %w", err)
			}
			token = strings.TrimSpace(string(data))
		}
		if !*dev && len(token) < 32 {
			return errors.New("provide --token-file with at least 32 random characters, or use --dev on loopback")
		}
		e, err := openEngine(*dbPath)
		if err != nil {
			return err
		}
		defer e.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		handler, err := server.New(ctx, e, server.Options{Token: token, Dev: *dev, NoWeb: *noWeb, Timezone: *timezone, Version: version, Assets: web.Assets(), TempDir: filepath.Dir(*dbPath), OAuthIssuer: *issuer, OAuthAudience: *audience, OAuthScope: *scope, WebOIDCClientID: *webClient, WebOIDCProviderName: *webProvider})
		cancel()
		if err != nil {
			return err
		}
		httpServer := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		stopped := make(chan os.Signal, 1)
		signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(stopped)
		go func() {
			<-stopped
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(ctx)
		}()
		slog.Info("Vrac ready", "address", *listen, "timezone", *timezone, "web", !*noWeb, "development", *dev)
		err = httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	output := flags.String("output", "", "Destination file; export defaults to standard output")
	input := flags.String("input", "", "JSON export to import into an empty database")
	if err := flags.Parse(args); err != nil {
		return flagError(err)
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	switch command {
	case "check", "backup", "export", "import", "reindex":
	default:
		return fmt.Errorf("unknown command %q; run vrac help", command)
	}
	e, err := openEngine(*dbPath)
	if err != nil {
		return err
	}
	defer e.Close()
	switch command {
	case "check":
		report, err := e.Check()
		if err != nil {
			return err
		}
		fmt.Printf("Nodes: %d\n", report.NodeCount)
		for _, issue := range report.Issues {
			fmt.Println(issue)
		}
		if len(report.Issues) > 0 {
			return errors.New("integrity check failed")
		}
		fmt.Println("Integrity: OK")
		return nil
	case "backup":
		if *output == "" {
			return errors.New("backup requires --output")
		}
		return e.Backup(*output)
	case "export":
		if *output == "" {
			return e.Export(os.Stdout)
		}
		return exportFile(e, *output)
	case "import":
		if *input == "" {
			return errors.New("import requires --input")
		}
		f, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer f.Close()
		return e.Import(f)
	case "reindex":
		return e.RebuildSearch()
	}
	return nil
}

func exportFile(e *engine.Engine, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("export destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(destination), ".vrac-export-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := e.Export(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Link(f.Name(), destination); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func openEngine(path string) (*engine.Engine, error) {
	if path == ":memory:" {
		return nil, errors.New("the service requires a file-backed database")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	return engine.Open(absolute)
}
func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func flagError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
