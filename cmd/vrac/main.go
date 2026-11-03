package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yimensar/vrac/internal/server"
	"github.com/yimensar/vrac/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	base = filepath.Join(base, "vrac")
	addr := flag.String("addr", "127.0.0.1:5235", "HTTP listen address")
	db := flag.String("db", filepath.Join(base, "vrac.db"), "SQLite database path")
	publicURL := flag.String("public-url", "", "Canonical browser/MCP URL (HTTPS required outside loopback)")
	web := flag.String("web", "web/dist", "Built React application directory")
	attachments := flag.String("attachments", filepath.Join(base, "attachments"), "Imported attachment directory")
	devOrigin := flag.String("dev-origin", "", "Explicit loopback Vite origin allowed for development")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	local := host == "localhost" || (ip != nil && ip.IsLoopback())
	if *publicURL == "" {
		if !local {
			return fmt.Errorf("remote binds require -public-url with an HTTPS URL")
		}
		*publicURL = "http://" + *addr
	}
	if !local && !strings.HasPrefix(*publicURL, "https://") {
		return fmt.Errorf("remote binds require an HTTPS public URL")
	}
	if err := os.MkdirAll(filepath.Dir(*db), 0700); err != nil {
		return err
	}
	s, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = os.Chmod(*db, 0600); err != nil {
		return err
	}
	srv, err := server.New(s, server.Config{PublicURL: *publicURL, OwnerPassword: os.Getenv("VRAC_OWNER_PASSWORD"), DevOrigin: *devOrigin, WebDir: *web, AttachmentDir: *attachments})
	if err != nil {
		return err
	}
	canonical, _ := url.Parse(*publicURL)
	publicIP := net.ParseIP(canonical.Hostname())
	publicIsLocal := canonical.Hostname() == "localhost" || (publicIP != nil && publicIP.IsLoopback())
	if (!local || !publicIsLocal) && srv.Auth.NeedsSetup() {
		return fmt.Errorf("remote first run requires VRAC_OWNER_PASSWORD (12 to 72 bytes)")
	}
	h := &http.Server{Addr: *addr, Handler: srv.Handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.Shutdown(shutdown)
	}()
	log.Printf("Vrac: %s (MCP: %s/mcp)", *publicURL, *publicURL)
	if err = h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
