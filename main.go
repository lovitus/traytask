package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
)

var version = "dev"

func main() {
	autoOpen := flag.Bool("open", true, "open dashboard on startup")
	listenAddr := flag.String("listen", "127.0.0.1:0", "dashboard listen address")
	flag.Parse()

	store, err := NewStore()
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	manager, err := NewManager(store)
	if err != nil {
		log.Fatalf("init manager: %v", err)
	}
	server, err := NewServer(manager)
	if err != nil {
		log.Fatalf("init web server: %v", err)
	}

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *listenAddr, err)
	}
	dashboardURL := fmt.Sprintf("http://%s", ln.Addr().String())
	log.Printf("version: %s", version)
	log.Printf("dashboard: %s", dashboardURL)
	log.Printf("data dir: %s", store.BaseDir())

	go func() {
		if err := http.Serve(ln, server.Routes()); err != nil {
			log.Printf("http server stopped: %v", err)
		}
	}()

	if *autoOpen {
		_ = openBrowser(dashboardURL)
	}

	tray := NewTrayApp(manager, dashboardURL)
	tray.Run()
}
