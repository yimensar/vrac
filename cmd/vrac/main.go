// Command vrac runs a Vrac instance and its maintenance commands.
package main

import (
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
  version          print the version

Configuration (flags override environment):
  -data DIR        VRAC_DATA        data directory (default ./data)
  -listen ADDR     VRAC_LISTEN      listen address (default 127.0.0.1:8080)
  -url URL         VRAC_PUBLIC_URL  public base URL (default http://localhost:PORT)
  -timezone ZONE   VRAC_TIMEZONE    IANA time zone of journal days (default local)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vrac:", err)
		os.Exit(1)
	}
}

type config struct {
	data, listen, publicURL, timezone string
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
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
	case "check":
		if err := e.Check(); err != nil {
			return err
		}
		fmt.Println("ok")
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}

func serve(e *engine.Engine, c config, loc *time.Location) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if c.publicURL == "" {
		port := c.listen[strings.LastIndex(c.listen, ":")+1:]
		c.publicURL = "http://localhost:" + port
	}
	srv, err := server.New(e, server.Options{PublicURL: c.publicURL, Version: version, Timezone: loc.String(), Web: web.Files(), Logger: log})
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
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.Shutdown(shutdown)
}
