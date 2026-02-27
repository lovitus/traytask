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
	webEnabled := flag.Bool("web", true, "enable web dashboard")
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
	log.Printf("version: %s", version)
	log.Printf("data dir: %s", store.BaseDir())
	dashboardURL := ""
	if *webEnabled {
		server, err := NewServer(manager)
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
