// Package storage keeps course files behind one interface so the backend can
// move between local disk and S3-compatible object storage (ADR-0001).
package storage

// Store holds course files by key.
type Store interface{}

// Local keeps files in a directory on disk.
type Local struct{ root string }

func NewLocal(root string) *Local { return &Local{root: root} }
