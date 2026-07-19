// study-win is the Windows desktop app: the web package served on localhost,
// wrapped in a WebView2 window so it looks and behaves like a native
// program — own window, own icon, no browser chrome, no console. Pure Go
// (the WebView2 bindings speak syscalls), so it cross-compiles from the
// usual Linux toolchain.
//
// Layout on disk: study.exe with a decks\ folder beside it (each entry a
// deck file or pack directory; explicit paths may also be passed as
// arguments). Progress lives in %LOCALAPPDATA%\study, and the WebView2
// profile (cookies, zoom) beside it, so state survives restarts.
//
// Runs in the web package's Local mode: one fixed identity, no guest
// cookies, no login UI.
//
//go:build windows

package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	webview "github.com/jchv/go-webview2"

	"study/web"
)

func main() {
	log.SetFlags(0)

	exe, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	base := filepath.Dir(exe)

	// The data dir also collects a log file: with -H=windowsgui there is no
	// console, so this is the only place errors can land.
	dataDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "study")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fatal(err)
	}
	if f, err := os.OpenFile(filepath.Join(dataDir, "study-win.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}

	// Decks: explicit arguments, else every entry of the decks folder
	// beside the exe.
	paths := os.Args[1:]
	if len(paths) == 0 {
		decksDir := filepath.Join(base, "decks")
		entries, err := os.ReadDir(decksDir)
		if err != nil || len(entries) == 0 {
			fatal(fmt.Errorf("no decks found — put deck packs in %s", decksDir))
		}
		for _, e := range entries {
			paths = append(paths, filepath.Join(decksDir, e.Name()))
		}
	}

	srv, err := web.New(paths, dataDir, "", nil)
	if err != nil {
		fatal(err)
	}
	srv.Local = true

	// An ephemeral localhost port: nothing to configure, nothing exposed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	go func() {
		if err := http.Serve(ln, srv); err != nil {
			fatal(err)
		}
	}()
	url := "http://" + ln.Addr().String()

	w := webview.NewWithOptions(webview.WebViewOptions{
		DataPath:  filepath.Join(dataDir, "webview"),
		AutoFocus: true,
		WindowOptions: webview.WindowOptions{
			Title:  "study",
			Width:  1000,
			Height: 720,
			IconId: 1, // the embedded icon resource (rsrc_windows_amd64.syso)
			Center: true,
		},
	})
	if w == nil {
		// No WebView2 runtime (unusual on updated Windows): fall back to
		// the default browser so the app still works, just less native.
		log.Printf("WebView2 unavailable; opening the default browser at %s", url)
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		select {}
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
}

// fatal logs and dies. With no console, the log file in %LOCALAPPDATA%\study
// is where the reason lands.
func fatal(err error) {
	log.Printf("study-win: %v", err)
	os.Exit(1)
}
