// Package secret encrypts the API keys Keine stores (site model keys, and
// later readers' own model keys) with the deployment's master key, so a
// copy of the database alone does not give the keys away.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// MinMasterKeyLen is the shortest master key accepted.
const MinMasterKeyLen = 32

var ErrUndecryptable = errors.New("secret: cannot decrypt (master key changed?)")

// Box seals and opens values with a key derived from the master key.
type Box struct{ aead cipher.AEAD }

func NewBox(masterKey string) (*Box, error) {
	if len(masterKey) < MinMasterKeyLen {
		return nil, fmt.Errorf("master key must be at least %d characters", MinMasterKeyLen)
	}
	key, err := hkdf.Key(sha256.New, []byte(masterKey), nil, "keine stored secrets v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain for the given purpose (e.g. "site_model:3"); Open
// only succeeds with the same purpose, so a sealed value cannot be moved
// to another row.
func (b *Box) Seal(plain, purpose string) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return b.aead.Seal(nonce, nonce, []byte(plain), []byte(purpose))
}

func (b *Box) Open(sealed []byte, purpose string) (string, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return "", ErrUndecryptable
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(purpose))
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plain), nil
}
