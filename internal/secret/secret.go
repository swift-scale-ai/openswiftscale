package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const keySize = 32

// Protector encrypts provider credentials before they are stored in SQLite.
// Its master key must be kept outside the database.
type Protector struct {
	aead cipher.AEAD
}

func LoadOrCreate(path, encodedKey string) (*Protector, error) {
	var key []byte
	var err error
	if strings.TrimSpace(encodedKey) != "" {
		key, err = base64.RawURLEncoding.DecodeString(strings.TrimSpace(encodedKey))
		if err != nil {
			return nil, fmt.Errorf("decode master key: %w", err)
		}
	} else {
		key, err = loadOrCreateKey(path)
		if err != nil {
			return nil, err
		}
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("master key must be %d bytes", keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Protector{aead: aead}, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("master key path is required")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create master key directory: %w", err)
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close master key: %w", err)
	}
	return key, nil
}

func (p *Protector) Encrypt(plaintext string, associatedData []byte) ([]byte, []byte, error) {
	if p == nil || p.aead == nil {
		return nil, nil, errors.New("secret protector is unavailable")
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return p.aead.Seal(nil, nonce, []byte(plaintext), associatedData), nonce, nil
}

func (p *Protector) Decrypt(ciphertext, nonce, associatedData []byte) (string, error) {
	if p == nil || p.aead == nil {
		return "", errors.New("secret protector is unavailable")
	}
	plaintext, err := p.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return "", errors.New("decrypt provider credential")
	}
	return string(plaintext), nil
}
