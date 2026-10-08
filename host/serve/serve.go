// Package serve is the karpo program without its database driver: what every deployment does
// once it has opened its database. The programs under cmd only choose the driver and the line
// that opens it.
//
// Configuration comes from the environment:
//
//	KARPO_JWT_SECRET               secret that signs the sessions, 32 characters at least (required)
//	KARPO_ADDR                     where to listen (":8080")
//	KARPO_EXPORTS_DIR              where the exported files wait (the default each program gives)
//	KARPO_DELIVER_EVERY            how often messages are carried between contexts ("2s")
//	KARPO_CHORES_EVERY             how often the chores run ("1m")
//	KARPO_BOOTSTRAP_ADMIN_USER     first administrator, created only while nobody administers
//	KARPO_BOOTSTRAP_ADMIN_PASSWORD its password, to be changed on the first session
package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// MinSecret is the least a secret that signs sessions may be.
const MinSecret = 32

// Commands of the program.
const (
	Migrate = "migrate" // applies the schema of every context
	Serve   = "serve"   // verifies the schema and serves
)

// Config is what the environment decides, whatever the database.
type Config struct {
	Secret  string
	Addr    string
	Exports string
	Deliver time.Duration
	Chores  time.Duration
}

// Env returns a variable of the environment, or a fallback.
func Env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func every(name, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(Env(name, fallback))
	if err != nil || d < 100*time.Millisecond {
		return 0, fmt.Errorf("%s: a duration of 100ms at least, like %s", name, fallback)
	}
	return d, nil
}

// Load reads the configuration; exports is where the exported files wait when the environment
// does not say.
func Load(exports string) (Config, error) {
	c := Config{Secret: os.Getenv("KARPO_JWT_SECRET"), Addr: Env("KARPO_ADDR", ":8080"), Exports: Env("KARPO_EXPORTS_DIR", exports)}
	var err1, err2, err3 error
	c.Deliver, err1 = every("KARPO_DELIVER_EVERY", "2s")
	c.Chores, err2 = every("KARPO_CHORES_EVERY", "1m")
	if len(c.Secret) < MinSecret {
		err3 = fmt.Errorf("KARPO_JWT_SECRET: a secret of %d characters at least is required", MinSecret)
	}
	return c, errors.Join(err3, err1, err2)
}

// Command returns the command the arguments of a program ask for: serve when they ask for none.
func Command(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return Serve
}

// Run does a command on an open database: it migrates, or it verifies the schema and serves until
// ctx ends. ping tells whether the database answers (the readiness probe).
func Run(ctx context.Context, command string, db *sqlrepo.DB, ping func(context.Context) error, cfg Config, logger log.Logger) error {
	migrator, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		return err
	}
	switch command {
	case Migrate:
		done, err := migrator.Migrate(ctx)
		if err != nil {
			return err
		}
		logger.Info("schema migrated", "contexts", len(host.Migrations()), "migrations", len(done))
		return nil
	case Serve:
	default:
		return fmt.Errorf("unknown command %q: %s or %s", command, Migrate, Serve)
	}
	// Never on a schema that is behind, or ahead of this program.
	if err := migrator.Verify(ctx); err != nil {
		return fmt.Errorf("the schema is not the one this program expects (run the %s command): %w", Migrate, err)
	}
	files, err := expinfra.NewDiskFiles(cfg.Exports)
	if err != nil {
		return err
	}
	h, err := host.Compose(hotswap.New(db), host.Options{JWTSecret: []byte(cfg.Secret), Files: files, ApiscoreEntity: os.Getenv("KARPO_APISCORE_ENTITY")})
	if err != nil {
		return err
	}
	started, err := h.Start(ctx)
	if err != nil {
		return err
	}
	logger.Info("karpo started", "database", db.Name(), "permissions", started.Permissions, "newFeatures", started.Features, "bootstrap", started.Bootstrap)

	health := distribution.NewHealthRegistry()
	health.RegisterReadinessProbe("database", ping)
	mux := http.NewServeMux()
	health.Mount(mux)
	mux.Handle("/", h.Handler())
	server := distribution.NewServer(cfg.Addr, mux)
	failed := server.Start()
	logger.Info("listening", "addr", cfg.Addr)

	go h.Run(ctx, cfg.Deliver, cfg.Chores, func(err error) { logger.Error("background work failed; it will be tried again", "error", err.Error()) })

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	logger.Info("stopping")
	stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return server.Shutdown(stop)
}
