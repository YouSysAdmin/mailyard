// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package postgres is the database backend: the pgx driver through
// database/sql, with a goose-driven schema from the repository's
// migrations directory.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/yousysadmin/mailyard/migrations"
)

// Postgres implements database.Database over a pgx connection pool.
type Postgres struct {
	db  *sql.DB
	dsn string

	// replicas are read-only followers. Nil unless the operator
	// configured any, and nothing reaches them by default - see
	// database.Base.
	replicas []*sql.DB
}

// Pool bounds the connection pool of every handle OpenWith creates,
// the primary and each replica alike. The zero value keeps the
// driver's own defaults: no cap on open connections and 2 idle -
// which is what every field's 0 means individually too.
type Pool struct {

	// MaxOpen caps concurrently open connections.
	MaxOpen int

	// MaxIdle is how many stay warm between uses.
	MaxIdle int

	// MaxLifetime recycles a connection this long after it opened.
	MaxLifetime time.Duration

	// MaxIdleTime closes a connection idle this long.
	MaxIdleTime time.Duration
}

// apply configures db, leaving driver defaults where a field is 0.
func (p Pool) apply(db *sql.DB) {
	if p.MaxOpen > 0 {
		db.SetMaxOpenConns(p.MaxOpen)
	}

	if p.MaxIdle > 0 {
		db.SetMaxIdleConns(p.MaxIdle)
	}

	if p.MaxLifetime > 0 {
		db.SetConnMaxLifetime(p.MaxLifetime)
	}

	if p.MaxIdleTime > 0 {
		db.SetConnMaxIdleTime(p.MaxIdleTime)
	}
}

// Open connects to the PRIMARY at dsn
// (postgres://user:pass@host:5432/dbname?sslmode=...), verifies the
// connection, runs pending goose migrations, and returns the handle.
func Open(dsn string) (*Postgres, error) {
	return OpenWithReplicas(dsn, nil)
}

// OpenWithReplicas is Open plus read-only followers.
//
// Migrations run on the PRIMARY and only there. A follower applies
// them through replication, and a goose run against one would fail on
// a read-only transaction anyway - which is the good outcome, because
// the bad one is a DSN list where somebody pasted the primary twice
// and two processes race to migrate.
//
// A replica that will not answer refuses the BOOT. The alternative -
// warn and carry on - means an operator who configured three
// followers gets two, silently, and finds out from a latency graph
// weeks later. Same reasoning as the SMTP listeners: a resource we
// were told to take and could not is a failure to start.
func OpenWithReplicas(dsn string, replicaDSNs []string) (*Postgres, error) {
	return OpenWith(dsn, replicaDSNs, true, Pool{})
}

// OpenWith is OpenWithReplicas with a say over whether this process
// MIGRATES.
//
// One node in a fleet runs `serve --init` and applies the schema, and
// every other node skips goose entirely. Two processes starting together
// against an empty database race each other, and the result is
// `relation "goose_db_version" does not exist` on one of them plus a
// duplicate-key error out of Postgres' own catalogue. A node that does
// not migrate also does not read the migration filesystem, parse it or
// take a transaction at boot, so the fleet starts faster and the schema
// has exactly one author.
//
// A node that skips does not verify the schema is current, deliberately.
// A check is a second thing that can be wrong, and an operator upgrading
// runs the init node first anyway.
func OpenWith(dsn string, replicaDSNs []string, migrate bool, pool Pool) (*Postgres, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	pool.apply(db)

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("ping postgres %q: %w", redactDSN(dsn), err)
	}

	if !migrate {
		// A node that does not migrate still has to say something
		// useful when nobody has. Without this the first failure is
		// whatever query runs first - `relation "settings" does not
		// exist` - which names neither the cause nor the fix, and is
		// exactly what a fresh `task run` produced.
		var ok bool
		if err := db.QueryRowContext(context.Background(),
			`SELECT to_regclass('goose_db_version') IS NOT NULL`).Scan(&ok); err != nil {
			_ = db.Close()

			return nil, fmt.Errorf("checking the schema: %w", err)
		}

		if !ok {
			_ = db.Close()

			return nil, fmt.Errorf(
				"the database has no schema and this node was not asked to create one - start exactly one node with `--init`")
		}
	}

	if migrate {
		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			_ = db.Close()

			return nil, fmt.Errorf("goose set dialect: %w", err)
		}

		if err := goose.Up(db, "."); err != nil {
			_ = db.Close()

			return nil, fmt.Errorf("goose up: %w", err)
		}
	}

	slog.Info("postgres open", "dsn", redactDSN(dsn), "migrated", migrate)

	replicas := make([]*sql.DB, 0, len(replicaDSNs))
	for _, rdsn := range replicaDSNs {
		r, rerr := sql.Open("pgx", rdsn)
		if rerr != nil {
			closeAll(db, replicas)

			return nil, fmt.Errorf("open replica: %w", rerr)
		}

		pool.apply(r)

		if rerr := r.PingContext(context.Background()); rerr != nil {
			_ = r.Close()
			closeAll(db, replicas)

			return nil, fmt.Errorf("ping replica %q: %w", redactDSN(rdsn), rerr)
		}

		replicas = append(replicas, r)
		slog.Info("postgres replica open", "dsn", redactDSN(rdsn))
	}

	return &Postgres{db: db, dsn: dsn, replicas: replicas}, nil
}

func closeAll(db *sql.DB, replicas []*sql.DB) {
	_ = db.Close()
	for _, r := range replicas {
		_ = r.Close()
	}
}

// Close stops the listener and releases its connection. Safe to call
// more than once.
func (p *Postgres) Close() error {
	for _, r := range p.replicas {
		_ = r.Close()
	}

	return p.db.Close()
}

// DB is the PRIMARY handle. Everything that is not explicitly a
// replica read uses it, including migrations, transactions and the
// LISTEN/NOTIFY connection.
func (p *Postgres) DB() *sql.DB { return p.db }

// Replicas returns the read-only followers, if any.
func (p *Postgres) Replicas() []*sql.DB { return p.replicas }

// redactDSN masks the password so the DSN is safe to put in logs and
// error messages. Falls back to a fixed placeholder when the DSN
// isn't URL-shaped (key=value DSNs may carry a password= pair we
// can't cheaply locate).
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return "(redacted dsn)"
	}

	return u.Redacted()
}
