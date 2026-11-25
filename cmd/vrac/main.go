// Command vrac runs a Vrac instance and its maintenance commands.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/yan-imensar/vrac/internal/engine"
	"github.com/yan-imensar/vrac/internal/server"
	"github.com/yan-imensar/vrac/web"
)

var version = "dev"

const usage = `Usage: vrac [command] [flags]

Commands:
  serve            run the server (default)
  backup PATH      write a consistent snapshot of the database
  export [PATH]    write a JSON export (stdout by default)
  import PATH      load a JSON export into an empty workspace
  check            run integrity checks
  password         set the owner password, and username with -user; creates
                   the owner if none (signs out every browser)
  passkeys reset   remove every passkey: the next sign-in enrolls a new one
  token NAME       create a personal access token for an MCP or API client
  version          print the version

Configuration (flags override environment):
  -data DIR        VRAC_DATA        data directory (default ./data)
  -listen ADDR     VRAC_LISTEN      listen address (default 127.0.0.1:8080)
  -url URL         VRAC_PUBLIC_URL  public base URL (default http://localhost:PORT)
  -timezone ZONE   VRAC_TIMEZONE    IANA time zone of journal days (default local)

Sign-in with an identity provider (OIDC, such as Pocket ID), optional:
  -oidc-issuer URL         VRAC_OIDC_ISSUER
  -oidc-client-id ID       VRAC_OIDC_CLIENT_ID
  -oidc-secret-file PATH   VRAC_OIDC_SECRET_FILE  (omit for a public PKCE client)
  -oidc-name NAME          VRAC_OIDC_NAME         (default "identity provider")
  -oidc-allow LIST         VRAC_OIDC_ALLOW        subjects or verified emails, comma separated
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vrac:", err)
		os.Exit(1)
	}
}

type config struct {
	data, listen, publicURL, timezone                       string
	user                                                    string
	oidcIssuer, oidcClient, oidcSecret, oidcName, oidcAllow string
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("vrac", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	var c config
	fs.StringVar(&c.data, "data", env("VRAC_DATA", "data"), "")
	fs.StringVar(&c.listen, "listen", env("VRAC_LISTEN", "127.0.0.1:8080"), "")
	fs.StringVar(&c.publicURL, "url", env("VRAC_PUBLIC_URL", ""), "")
	fs.StringVar(&c.timezone, "timezone", env("VRAC_TIMEZONE", ""), "")
	fs.StringVar(&c.user, "user", "", "")
	fs.StringVar(&c.oidcIssuer, "oidc-issuer", env("VRAC_OIDC_ISSUER", ""), "")
	fs.StringVar(&c.oidcClient, "oidc-client-id", env("VRAC_OIDC_CLIENT_ID", ""), "")
	fs.StringVar(&c.oidcSecret, "oidc-secret-file", env("VRAC_OIDC_SECRET_FILE", ""), "")
	fs.StringVar(&c.oidcName, "oidc-name", env("VRAC_OIDC_NAME", ""), "")
	fs.StringVar(&c.oidcAllow, "oidc-allow", env("VRAC_OIDC_ALLOW", ""), "")
	// Flags may come before or after positional arguments.
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
	switch cmd {
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	loc := time.Local
	if c.timezone != "" {
		l, err := time.LoadLocation(c.timezone)
		if err != nil {
			return fmt.Errorf("time zone: %w", err)
		}
		loc = l
	}
	e, err := engine.Open(filepath.Join(c.data, "vrac.sqlite"), engine.Options{Location: loc})
	if err != nil {
		return err
	}
	defer e.Close()
	switch cmd {
	case "serve":
		return serve(e, c, loc)
	case "backup":
		if len(rest) != 1 {
			return errors.New("backup needs a destination path")
		}
		return e.Backup(rest[0])
	case "export":
		out := os.Stdout
		if len(rest) == 1 {
			f, err := os.OpenFile(rest[0], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			defer f.Close()
			out = f
		}
		return e.Export(context.Background(), out)
	case "import":
		if len(rest) != 1 {
			return errors.New("import needs an export file")
		}
		f, err := os.Open(rest[0])
		if err != nil {
			return err
		}
		defer f.Close()
		return e.Import(context.Background(), f)
	case "password":
		password, err := readPassword()
		if err != nil {
			return err
		}
		if err := server.ResetPassword(context.Background(), e, c.user, password); err != nil {
			return err
		}
		fmt.Println("password changed; sign in again")
		return nil
	case "passkeys":
		if len(rest) != 1 || rest[0] != "reset" {
			return errors.New("usage: vrac passkeys reset")
		}
		n, err := e.ResetPasskeys(context.Background())
		if err != nil {
			return err
		}
		fmt.Printf("%d passkeys removed; the next sign-in enrolls a new one\n", n)
		return nil
	case "token":
		if len(rest) != 1 {
			return errors.New("token needs a name, such as \"Claude Code\"")
		}
		token, err := server.CreateToken(context.Background(), e, rest[0])
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	case "check":
		if err := e.Check(); err != nil {
			return err
		}
		fmt.Println("ok")
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// readPassword asks twice without echo on a terminal, or reads one line
// from standard input otherwise.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Repeat: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("the passwords differ")
	}
	return string(first), nil
}

func serve(e *engine.Engine, c config, loc *time.Location) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if c.publicURL == "" {
		port := c.listen[strings.LastIndex(c.listen, ":")+1:]
		c.publicURL = "http://localhost:" + port
	}
	opts := server.Options{PublicURL: c.publicURL, Version: version, Timezone: loc.String(), Web: web.Files(), Logger: log}
	if c.oidcIssuer != "" {
		o := &server.OIDC{Issuer: c.oidcIssuer, ClientID: c.oidcClient, Name: c.oidcName}
		for _, a := range strings.Split(c.oidcAllow, ",") {
			if a = strings.TrimSpace(a); a != "" {
				o.Allowed = append(o.Allowed, a)
			}
		}
		if c.oidcSecret != "" {
			b, err := os.ReadFile(c.oidcSecret)
			if err != nil {
				return fmt.Errorf("OIDC secret: %w", err)
			}
			o.ClientSecret = strings.TrimSpace(string(b))
		}
		opts.OIDC = o
	}
	srv, err := server.New(e, opts)
	if err != nil {
		return err
	}
	h := &http.Server{Addr: c.listen, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { errs <- h.ListenAndServe() }()
	log.Info("vrac is running", "url", c.publicURL, "listen", c.listen, "mcp", c.publicURL+"/mcp", "web", web.Files() != nil, "version", version)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	// A second Ctrl+C now ends the process at once.
	stop()
	log.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(shutdown); err != nil {
		// Open streams (such as MCP clients) do not delay the exit further.
		return h.Close()
	}
	return nil
}
