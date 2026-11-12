package storage_test

import (
	"testing"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/storage/storagetest"
)

func TestLocalStore(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Store { return storage.NewLocal(t.TempDir()) })
}
