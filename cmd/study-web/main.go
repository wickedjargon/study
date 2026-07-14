// study-web — the browser frontend to study, a demo for friends: guests get
// a cookie identity and their own progress, no account needed.
//
// Usage: study-web [flags] <deck-or-dir>...
//
// Each argument is either a deck itself (a *.deck file, or a pack directory
// named *.deck/) or a directory whose deck entries are all served.
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
	data := flag.String("data", "data", "directory for per-guest progress")
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"examples"}
	}

	srv, err := web.New(paths, *data)
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
