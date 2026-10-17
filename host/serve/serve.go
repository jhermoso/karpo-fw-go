// Package serve es el programa karpo sin su driver de base de datos: lo que hace cada despliegue
// una vez abierta su base de datos. Los programas de cmd sólo eligen el driver y la línea que la
// abre.
//
// La configuración viene del entorno:
//
//	KARPO_JWT_SECRET               secreto que firma las sesiones, 32 caracteres como mínimo (obligatorio)
//	KARPO_ADDR                     dónde escuchar (":8080")
//	KARPO_EXPORTS_DIR              dónde esperan los ficheros exportados (el valor por defecto que da cada programa)
//	KARPO_DELIVER_EVERY            cada cuánto se llevan los mensajes entre contextos ("2s")
//	KARPO_CHORES_EVERY             cada cuánto se ejecutan las tareas ("1m")
//	KARPO_BOOTSTRAP_ADMIN_USER     primer administrador, creado sólo mientras nadie administra
//	KARPO_BOOTSTRAP_ADMIN_PASSWORD su contraseña, que se cambia en la primera sesión
package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// MinSecret es lo mínimo que puede medir un secreto que firma sesiones.
const MinSecret = 32

// Comandos del programa.
const (
	Migrate = "migrate" // aplica el esquema de cada contexto
	Serve   = "serve"   // verifica el esquema y sirve
)

// Config es lo que decide el entorno, sea cual sea la base de datos.
type Config struct {
	Secret  string
	Addr    string
	Exports string
	Deliver time.Duration
	Chores  time.Duration
	// AlertWebhook es donde se avisa de lo que necesita a una persona (KARPO_ALERT_WEBHOOK); vacío: sólo el log.
	AlertWebhook string
}

// Env devuelve una variable del entorno, o un valor de reserva.
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

// Load lee la configuración; exports es donde esperan los ficheros exportados cuando el entorno no
// lo dice.
func Load(exports string) (Config, error) {
	c := Config{Secret: os.Getenv("KARPO_JWT_SECRET"), Addr: Env("KARPO_ADDR", ":8080"), Exports: Env("KARPO_EXPORTS_DIR", exports),
		AlertWebhook: os.Getenv("KARPO_ALERT_WEBHOOK")}
	var err1, err2, err3, err4 error
	if c.AlertWebhook != "" {
		if u, err := url.Parse(c.AlertWebhook); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			err4 = errors.New("KARPO_ALERT_WEBHOOK: an http or https address")
		}
	}
	c.Deliver, err1 = every("KARPO_DELIVER_EVERY", "2s")
	c.Chores, err2 = every("KARPO_CHORES_EVERY", "1m")
	if len(c.Secret) < MinSecret {
		err3 = fmt.Errorf("KARPO_JWT_SECRET: a secret of %d characters at least is required", MinSecret)
	}
	return c, errors.Join(err3, err1, err2, err4)
}

// Command devuelve el comando que piden los argumentos de un programa: serve cuando no piden ninguno.
func Command(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return Serve
}

// Run ejecuta un comando sobre una base de datos abierta: migra, o verifica el esquema y sirve
// hasta que acaba ctx. ping dice si la base de datos responde (la sonda de disponibilidad).
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
	// Nunca sobre un esquema que va por detrás, o por delante, de este programa.
	if err := migrator.Verify(ctx); err != nil {
		return fmt.Errorf("the schema is not the one this program expects (run the %s command): %w", Migrate, err)
	}
	files, err := expinfra.NewDiskFiles(cfg.Exports)
	if err != nil {
		return err
	}
	h, err := host.Compose(hotswap.New(db), host.Options{JWTSecret: []byte(cfg.Secret), Files: files,
		OnDeliveryGivenUp: Alerts{Webhook: cfg.AlertWebhook, Logger: logger}.DeliveryGivenUp})
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
