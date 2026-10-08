package memory_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

func TestRepositoryConformance(t *testing.T) {
	repotest.Run(t, repotest.Harness{
		EvaluatesInMemory: true,
		New: func(t *testing.T) (domain.Repository[repotest.WidgetID, *repotest.Widget], domain.UnitOfWork) {
			store := memory.NewStore("conformance")
			repo := memory.NewRepository[repotest.WidgetID, *repotest.Widget](store,
				memory.WithClone((*repotest.Widget).Clone))
			return repo, store
		},
	})
}

func TestRepositoryConformance_DefaultShallowClone(t *testing.T) {
	repotest.Run(t, repotest.Harness{
		EvaluatesInMemory: true,
		New: func(t *testing.T) (domain.Repository[repotest.WidgetID, *repotest.Widget], domain.UnitOfWork) {
			store := memory.NewStore("conformance")
			return memory.NewRepository[repotest.WidgetID, *repotest.Widget](store), store
		},
	})
}
