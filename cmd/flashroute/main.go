package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/server"
	"github.com/uiautodev/flashroute/internal/store"
)

var version = "1.0.0"

func main() {
	listen := flag.String("listen", ":8080", "address to listen on")
	dataDir := flag.String("data-dir", store.DefaultDir(), "data directory")
	forceAuth := flag.Bool("require-auth", false, "require the password even for loopback requests (useful behind a reverse proxy)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	st, err := store.New(*dataDir)
	if err != nil {
		log.Fatalf("load store: %v", err)
	}
	gw := gateway.New(st)
	srv := server.New(st, gw, server.Options{
		ForceAuth: *forceAuth,
		Version:   version,
		DataDir:   st.Dir(),
	})

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	httpServer := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	url := "http://" + displayAddr(ln.Addr().String())
	log.Printf("FlashRoute %s", version)
	log.Printf("data: %s", st.Dir())
	log.Printf("open: %s", url)
	log.Printf("gateway: %s/v1  (API key required)", strings.TrimRight(url, "/"))
	if !isLoopbackListen(ln.Addr()) {
		log.Printf("remote access requires a password; open the web UI to configure it")
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		log.Fatalf("serve: %v", err)
	case <-sig:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	if strings.HasPrefix(addr, "[::]:") {
		return "localhost:" + strings.TrimPrefix(addr, "[::]:")
	}
	return addr
}

func isLoopbackListen(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return false
	}
	return tcp.IP.IsLoopback()
}
