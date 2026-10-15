package host_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// tradeHost is a started host with a global administrator, for the scenarios that build their own
// companies, people and documents.
type tradeHost struct {
	t    *testing.T
	h    *host.Host
	actx context.Context
}

func newTradeHost(t *testing.T, sw *hotswap.Switch) *tradeHost {
	ctx := context.Background()
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("trade-host-test-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "tester", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	return &tradeHost{t: t, h: h, actx: authz.WithContext(ctx, admin)}
}

func memorySwitch(t *testing.T) *hotswap.Switch {
	store := memory.NewStore("memory")
	if err := geoinfra.LoadMemory(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return sw
}

func sqliteSwitch(t *testing.T) *hotswap.Switch {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "karpo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return hotswap.New(db)
}
