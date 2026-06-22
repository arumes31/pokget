package service

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("mock read error")
}

func TestCryptoService_InvalidKey(t *testing.T) {
	_, err := NewCryptoService("short_key")
	if err == nil {
		t.Error("Expected error for invalid key length")
	}
}

func TestCryptoService_KeyError(t *testing.T) {
	s := &CryptoService{key: []byte("short")} // manually bypass constructor

	_, err := s.Encrypt("test")
	if err == nil {
		t.Error("Expected error from aes.NewCipher in Encrypt")
	}

	_, err = s.Decrypt(base64.StdEncoding.EncodeToString([]byte("some data")))
	if err == nil {
		t.Error("Expected error from aes.NewCipher in Decrypt")
	}
}

func TestCryptoService_Decrypt_OpenError(t *testing.T) {
	key := "12345678901234567890123456789012"
	s, err := NewCryptoService(key)
	if err != nil {
		t.Fatalf("Failed to create CryptoService: %v", err)
	}

	ciphertext, err := s.Encrypt("test data")
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	// Tamper with the base64 encoded ciphertext
	data, _ := base64.StdEncoding.DecodeString(ciphertext)
	data[len(data)-1] ^= 0xff // flip bits in the MAC to ensure open fails
	tamperedCiphertext := base64.StdEncoding.EncodeToString(data)

	_, err = s.Decrypt(tamperedCiphertext)
	if err == nil {
		t.Error("Expected error from gcm.Open for tampered ciphertext")
	}
}

func TestCryptoService_Encrypt_RandError(t *testing.T) {
	key := "12345678901234567890123456789012"
	s, err := NewCryptoService(key)
	if err != nil {
		t.Fatalf("Failed to create CryptoService: %v", err)
	}

	// Save original rand.Reader and restore after test
	originalReader := rand.Reader
	defer func() { rand.Reader = originalReader }()

	// Swap rand.Reader with our mock
	rand.Reader = &errorReader{}

	_, err = s.Encrypt("test data")
	if err == nil {
		t.Error("Expected error from rand.Reader")
	}
}
