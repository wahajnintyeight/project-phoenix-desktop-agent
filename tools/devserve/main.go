// Command devserve serves the built UI (internal/ui/dist) over HTTP for `wails3 dev`.
// In dev mode Wails proxies the page to a frontend server; Phoenix's UI is a single built
// file, so this is just a static file server that reads from disk on every request.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
)

func main() {
	port := flag.Int("port", 9245, "port to listen on (loopback only)")
	dir := flag.String("dir", "internal/ui/dist", "directory to serve")
	flag.Parse()

	h := http.FileServer(http.Dir(*dir))
	// The UI changes between rebuilds, so never let a stale copy be cached.
	nocache := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
	// "localhost" may resolve to either loopback address, so listen on both.
	errc := make(chan error, 2)
	listening := 0
	for _, host := range []string{"127.0.0.1", "[::1]"} {
		l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, *port))
		if err != nil {
			log.Printf("not listening on %s: %v", host, err)
			continue
		}
		listening++
		log.Printf("serving %s on http://%s", *dir, l.Addr())
		go func() { errc <- http.Serve(l, nocache) }()
	}
	if listening == 0 {
		log.Fatal("could not listen on any loopback address")
	}
	log.Fatal(<-errc)
}
