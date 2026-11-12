package storage_test

import (
	"testing"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/storage/storagetest"
	"github.com/Bank5y/keine/internal/testutil"
)

func TestS3Store(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Store { return testutil.NewS3Store(t) })
}
