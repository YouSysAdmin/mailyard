// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Writes the generated half of every client. The work
// is in internal/sdkgen, so a test can run the same code in memory and
// fail when the files on disk have fallen behind.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yousysadmin/mailyard/internal/sdkgen"
)

func main() {
	files, err := sdkgen.Render()
	if err != nil {
		// Write what there is anyway: a compile error in a file you can
		// read beats an error message about a file you cannot.
		for name, src := range files {
			_ = os.WriteFile(filepath.Join(sdkgen.Dir, name), []byte(src), 0o644)
		}

		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(sdkgen.Dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		os.Exit(1)
	}

	for name, src := range files {
		if err := os.WriteFile(filepath.Join(sdkgen.Dir, name), []byte(src), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "sdkgen:", err)
			os.Exit(1)
		}
	}

	fmt.Printf("sdkgen: %d files -> %s\n", len(files), sdkgen.Dir)

	// The script clients. Same routes, same method names, no generated
	// types - see internal/sdkgen/script.go.
	for dir, out := range map[string]map[string]string{
		sdkgen.PythonDir: sdkgen.RenderPython(),
		sdkgen.RubyDir:   sdkgen.RenderRuby(),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "sdkgen:", err)
			os.Exit(1)
		}

		for name, src := range out {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "sdkgen:", err)
				os.Exit(1)
			}
		}

		fmt.Printf("sdkgen: %d file(s) -> %s\n", len(out), dir)
	}
}
