// study-web — the browser frontend to study, a demo for friends: guests get
// a cookie identity and their own progress, no account needed. Logging in
// (a magic link by email) makes that progress portable across devices.
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
	data := flag.String("data", "data", "directory for per-visitor progress and identity")
	baseURL := flag.String("base-url", "", "public URL login links point at (default http://<addr>)")
	local := flag.Bool("local", false, "single-user desktop mode: fixed identity, no login UI")
	mailFrom := flag.String("mail-from", "study <login@study.fftp.io>", "sender for login emails")
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"examples"}
	}
	if *baseURL == "" {
		*baseURL = "http://" + *addr
	}

	// With a Resend key login links go out as real email; without one they
	// land in the log, which is all local development needs.
	var mailer web.Mailer = web.LogMailer{}
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		mailer = web.ResendMailer{Key: key, From: *mailFrom}
	} else {
		log.Printf("RESEND_API_KEY not set; login links go to this log")
	}

	srv, err := web.New(paths, *data, *baseURL, mailer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
	srv.Local = *local

	log.Printf("study-web listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}
