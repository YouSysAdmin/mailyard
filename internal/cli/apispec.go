// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/mailyard/internal/openapi"
)

// newExportAPISpecCmd writes the OpenAPI descriptions to disk.
//
// A command rather than an endpoint. The documents are derived from
// the Go types this binary was built with, so they change only when
// the binary does - serving them would mean holding them in memory,
// and gating them would mean a config key, both for a request that
// arrives about once a year. Running the binary you already have
// produces the document that matches it, which is the same guarantee
// an endpoint gave.
func newExportAPISpecCmd() *cobra.Command {
	var (
		out     string
		surface string
	)
	cmd := &cobra.Command{
		Use:   "export-api-spec",
		Short: "Write the OpenAPI descriptions of this build",
		Long: "Writes the OpenAPI descriptions generated from the response types this\n" +
			"binary returns.\n\n" +
			"  api  the machine surface (/api/v1), what integrators build against\n" +
			"  app  the console surface (/api), which belongs to the web UI and\n" +
			"       moves with it\n\n" +
			"With no --out the document goes to stdout, which only works for one\n" +
			"surface at a time.",
		RunE: func(cmd *cobra.Command, args []string) error {
			wanted, err := surfaces(surface)
			if err != nil {
				return err
			}

			if out == "" && len(wanted) > 1 {
				return fmt.Errorf("writing several surfaces to stdout would run them together, pass --out or --surface")
			}

			for _, s := range wanted {
				doc, err := build(s)
				if err != nil {
					return err
				}

				if out == "" {
					_, err := cmd.OutOrStdout().Write(doc)
					if err != nil {
						return err
					}

					continue
				}

				path := out
				if len(wanted) > 1 {
					path = withSuffix(out, s)
				}

				if err := os.WriteFile(path, doc, 0o644); err != nil {
					return fmt.Errorf("writing %s: %w", path, err)
				}

				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%s surface, %d bytes)\n", path, s, len(doc))
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "file to write (default stdout)")
	cmd.Flags().StringVar(&surface, "surface", "api", "which surface: api, app, or all")

	return cmd
}

func surfaces(s string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "api":
		return []string{"api"}, nil
	case "app", "console":
		return []string{"app"}, nil
	case "all", "both":
		return []string{"api", "app"}, nil
	default:
		return nil, fmt.Errorf("unknown surface %q: want api, app or all", s)
	}
}

func build(surface string) ([]byte, error) {
	if surface == "app" {
		return openapi.ConsoleSpec()
	}

	return openapi.MachineSpec()
}

// withSuffix turns out.yaml into out.api.yaml, so exporting both
// surfaces at once does not have one overwrite the other.
func withSuffix(path, surface string) string {
	ext := filepath.Ext(path)

	return strings.TrimSuffix(path, ext) + "." + surface + ext
}
