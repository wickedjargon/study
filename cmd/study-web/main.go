// study-web — the browser frontend to study, a demo for friends: guests get
// a cookie identity and their own progress, no account needed.
//
// Usage: study-web [flags]
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"study/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8091", "listen address")
	decks := flag.String("decks", "examples", "directory of deck files / pack directories to serve")
	data := flag.String("data", "data", "directory for per-guest progress")
	flag.Parse()

	srv, err := web.New(*decks, *data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	log.Printf("study-web listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}
