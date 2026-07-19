// study-gio is the Gio-rendered preview of the desktop app: the same engine
// and progress files as study, drawn with a cross-platform toolkit. It runs
// side by side with the X11 build during the transition; see gio/app.go for
// what this first slice covers.
package main

import (
	"fmt"
	"os"

	"study/gio"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		fmt.Fprintln(os.Stderr, "usage: study-gio <deck-file | pack-directory>")
		os.Exit(2)
	}
	if err := gio.Run(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "study-gio: %v\n", err)
		os.Exit(1)
	}
}
