package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

var version = "dev"

func main() {
	autoOpen := flag.Bool("open", true, "open dashboard on startup")
	webEnabled := flag.Bool("web", true, "enable web dashboard")
	listenAddr := flag.String("listen", "127.0.0.1:0", "dashboard listen address")
	allowRemote := flag.Bool("allow-remote-web", false, "allow web dashboard to listen on non-loopback addresses")
	flag.Parse()

	exitNow, err := ensureInstalledAndRelaunch()
	if err != nil {
		log.Printf("install bootstrap warning: %v", err)
	}
	if exitNow {
		return
	}

	store, err := NewStore()
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	manager, err := NewManager(store)
	if err != nil {
		log.Fatalf("init manager: %v", err)
	}
	log.Printf("version: %s", version)
	log.Printf("data dir: %s", store.BaseDir())
	dashboardURL := ""
	if *webEnabled {
		if !*allowRemote && !isLoopbackListenAddr(*listenAddr) {
			log.Fatalf("refusing non-loopback listen addr %q; use -allow-remote-web=true to override", *listenAddr)
		}
		apiToken, err := generateAPIToken()
		if err != nil {
			log.Fatalf("generate api token: %v", err)
		}
		server, err := NewServer(manager, apiToken)
		if err != nil {
			log.Fatalf("init web server: %v", err)
		}
		ln, err := net.Listen("tcp", *listenAddr)
		if err != nil {
			log.Fatalf("listen %s: %v", *listenAddr, err)
		}
		dashboardURL = fmt.Sprintf("http://%s", ln.Addr().String())
		log.Printf("dashboard: %s", dashboardURL)
		go func() {
			if err := http.Serve(ln, server.Routes()); err != nil {
				log.Printf("http server stopped: %v", err)
			}
		}()
		if *autoOpen {
			_ = openBrowser(dashboardURL)
		}
	} else {
		log.Printf("dashboard: disabled (-web=false)")
	}

	tray := NewTrayApp(manager, dashboardURL, store.BaseDir())
	tray.Run()
}

func generateAPIToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
