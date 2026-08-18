// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// newTLSCmd builds `mailyard tls`, the way back in.
//
// Which certificate a listener serves lives in the database, not in a
// config file - one source of truth, changeable without a restart. The
// cost is that assigning a certificate the console cannot be reached
// through leaves the console as the only tool that could undo it.
//
// So these are the same three operations, offline, against the database
// the config names, and they work whether the server is up. Same
// reasoning as set-password.
func newTLSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls",
		Short: "Inspect and change which certificate each listener serves",
		Long: "Read and write listener certificate assignments directly in the\n" +
			"database, without a running server.\n\n" +
			"Use this to recover from an assignment that made the console\n" +
			"unreachable - the console is otherwise the only place these are set.",
	}
	cmd.AddCommand(newTLSStatusCmd(), newTLSAssignCmd(), newTLSUnassignCmd())

	return cmd
}

func newTLSStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what each listener terminates and serves",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, st, closeDB, err := openForTLS(cmd)
			if err != nil {
				return err
			}

			defer closeDB()

			ctx := context.Background()
			assigned, err := assignments(ctx, st)
			if err != nil {
				return err
			}

			names, err := st.Certificate.ListScope(ctx, certmodel.ScopeManaged)
			if err != nil {
				return fmt.Errorf("list certificates: %w", err)
			}

			_, _ = fmt.Fprintf(os.Stdout, "%-12s  %-9s  %s\n", "LISTENER", "TLS", "CERTIFICATE")
			for _, l := range []string{
				certificate.ListenerServer,
				certificate.ListenerSubmission,
				certificate.ListenerInbound,
			} {
				what := assigned[l]
				if what == "" {
					// Naming the fallback rather than printing a blank.
					// The question being asked is "what is on the wire", and
					// nothing assigned is an answer, not an absence.
					// Deliberately not resolved further.
					// Whether ACME applies is a platform setting read at handshake
					// time, and this command runs with no process to ask.
					what = "(nothing assigned: acme, then the self-signed pair)"
				}

				state := "off"
				if certificate.TerminatesTLS(cfg, l) {
					state = "on"
				}

				_, _ = fmt.Fprintf(os.Stdout, "%-12s  %-9s  %s\n", l, state, what)
			}

			if len(names) == 0 {
				_, _ = fmt.Fprintf(os.Stdout, "\nno managed certificates stored\n")

				return nil
			}

			_, _ = fmt.Fprintf(os.Stdout, "\nmanaged certificates:\n")
			for _, r := range names {
				kind := ""
				if d, derr := certmodel.ParseDetails(r.CertPEM); derr == nil && d != nil && d.IsCA {
					kind = "  (authority - cannot be assigned to a listener)"
				}

				_, _ = fmt.Fprintf(os.Stdout, "  %s%s\n", r.Name, kind)
			}

			return nil
		},
	}
}

func newTLSAssignCmd() *cobra.Command {
	var listener, name string

	cmd := &cobra.Command{
		Use:   "assign",
		Short: "Point a listener at a stored certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if certificate.SettingFor(listener) == "" {
				return fmt.Errorf("--listener must be one of %s, %s, %s",
					certificate.ListenerServer, certificate.ListenerSubmission, certificate.ListenerInbound)
			}

			if name = strings.TrimSpace(name); name == "" {
				return errors.New("--certificate is required (use `tls unassign` to clear one)")
			}

			cfg, st, closeDB, err := openForTLS(cmd)
			if err != nil {
				return err
			}

			defer closeDB()

			ctx := context.Background()
			rec, err := st.Certificate.GetPublic(ctx, certmodel.ScopeManaged, name)
			if err != nil {
				return fmt.Errorf("look up %s: %w", name, err)
			}

			if rec == nil {
				// Refused here, unlike over the API, where assigning before
				// uploading is a reasonable order to work in.
				// Somebody reaching for this command is recovering, and the last
				// thing that helps is a typo that looks like it worked.
				return fmt.Errorf("no managed certificate called %s - run `mailyard tls status` to list them", name)
			}

			// The same rule the settings API applies, from the same function.
			// A second copy here is how the console and the
			// command line come to disagree about what may be served.
			if err := certificate.ValidateAssignment(ctx, st.Certificate, name); err != nil {
				return err
			}

			if err := putSetting(ctx, st, certificate.SettingFor(listener), name); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "%s now serves %s\n", listener, name)
			if !certificate.TerminatesTLS(cfg, listener) {
				// The assignment is recorded and nothing will present it,
				// which is the whole point of this warning - silently it
				// reads as a certificate in use.
				fmt.Fprintf(os.Stderr,
					"warning: %s does not terminate TLS, so nothing presents this yet - set %s.tls.enabled\n",
					listener, listener)
			}

			fmt.Fprint(os.Stderr, convergenceNote)

			return nil
		},
	}
	cmd.Flags().StringVar(&listener, "listener", "", "server, submission or inbound")
	cmd.Flags().StringVar(&name, "certificate", "", "name of a stored managed certificate")

	return cmd
}

func newTLSUnassignCmd() *cobra.Command {
	var listener string

	cmd := &cobra.Command{
		Use:   "unassign",
		Short: "Drop a listener back to the rest of the chain",
		Long: "Clear a listener's assignment. It then serves the ACME certificate\n" +
			"if one is configured for its hostname, and otherwise the self-signed\n" +
			"pair - so it keeps serving TLS either way.\n\n" +
			"This is the recovery path: an assignment that broke the console is\n" +
			"undone here without one.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if certificate.SettingFor(listener) == "" {
				return fmt.Errorf("--listener must be one of %s, %s, %s",
					certificate.ListenerServer, certificate.ListenerSubmission, certificate.ListenerInbound)
			}

			_, st, closeDB, err := openForTLS(cmd)
			if err != nil {
				return err
			}

			defer closeDB()

			ctx := context.Background()
			if err := st.Setting.Delete(ctx, certificate.SettingFor(listener)); err != nil {
				return fmt.Errorf("clear the assignment: %w", err)
			}

			fmt.Fprintf(os.Stderr,
				"%s is unassigned and falls back to the rest of the chain\n", listener)
			fmt.Fprint(os.Stderr, convergenceNote)

			return nil
		},
	}
	cmd.Flags().StringVar(&listener, "listener", "", "server, submission or inbound")

	return cmd
}

// convergenceNote says how long a running node takes to notice.
//
// FIVE minutes, not thirty seconds, and the difference was measured
// rather than assumed.
// The certificate itself is re-resolved every 30 seconds, but the name
// comes from the settings cache, which a node reloads on a five-minute tick - and a
// write made out here reaches neither directly, because nothing tells the node it happened.
//
// Not thirty seconds, which is what the convergence window suggests.
// In an emergency, an operator who watches a console stay broken for four
// minutes past a deadline they were given has no way to tell a slow fix
// from a failed one, which is worse than being told to restart.
const convergenceNote = "a running node picks this up within 5 minutes (its settings refresh), " +
	"or restart it to apply immediately\n"

// openForTLS loads the config and opens the database read-write.
//
// Migrations are not applied: this is a one-shot command against a
// database somebody else migrates, and recovering a listener must not
// become a schema change. Same call shape as set-password.
func openForTLS(cmd *cobra.Command) (*env.Config, *store.Store, func(), error) {
	configPath, _ := cmd.Flags().GetString("config")
	cfg, err := env.Load(configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, nil, fmt.Errorf("config invalid: %w", err)
	}

	db, st, err := openDatabase(&cfg.Database,
		crypto.New(cfg.Database.Crypto.EncryptionKey), false)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open db: %w", err)
	}

	return cfg, st, func() { _ = db.Close() }, nil
}

// assignments reads what each listener is pointed at, straight from the
// table rather than through the settings cache - there is no process here to have warmed one.
func assignments(ctx context.Context, st *store.Store) (map[string]string, error) {
	rows, err := st.Setting.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}

	byKey := make(map[string]string, len(rows))
	for _, r := range rows {
		byKey[r.Key] = r.Value
	}

	out := map[string]string{}
	for _, l := range []string{
		certificate.ListenerServer,
		certificate.ListenerSubmission,
		certificate.ListenerInbound,
	} {
		out[l] = byKey[certificate.SettingFor(l)]
	}

	return out, nil
}

// putSetting writes one value the way the settings API does, including
// the rule that a value equal to the default is stored as the ABSENCE of a row.
// The default for every assignment is empty, so this only ever writes - but the rule is honoured rather than restated.
func putSetting(ctx context.Context, st *store.Store, key, value string) error {
	def, ok := smodel.Lookup(key)
	if !ok {
		return fmt.Errorf("unknown setting %s", key)
	}

	if value == def.Default {
		return st.Setting.Delete(ctx, key)
	}

	return st.Setting.Put(ctx, &smodel.Setting{
		Key:       def.Key,
		Value:     value,
		Type:      def.Type,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: "mailyard tls",
	})
}
