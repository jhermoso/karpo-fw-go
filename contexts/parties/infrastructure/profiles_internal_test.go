package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// En memoria, el perfil guardado en una unidad de trabajo que falla se deshace, como el resto de
// lo que se cambia en ella.
func TestMemoryProfiles_RollBackWithTheUnitOfWork(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore("memory")
	profiles, err := ProfilesFor(store)
	if err != nil {
		t.Fatal(err)
	}
	org := domain.NewPartyID()
	save := func(tz string, fail error) error {
		return store.Do(ctx, func(ctx context.Context) error {
			if err := profiles.Save(ctx, domain.InternalOrganizationProfile{Organization: org, TimeZone: tz}); err != nil {
				return err
			}
			return fail
		})
	}
	boom := errors.New("boom")
	if err := save("Atlantic/Canary", boom); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if p, _ := profiles.Get(ctx, org); p.TimeZone != "" {
		t.Fatalf("a new profile is undone: %+v", p)
	}
	if err := save("Europe/Madrid", nil); err != nil {
		t.Fatal(err)
	}
	if err := save("Atlantic/Canary", boom); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if p, _ := profiles.Get(ctx, org); p.TimeZone != "Europe/Madrid" {
		t.Fatalf("a change is undone: %+v", p)
	}
}
