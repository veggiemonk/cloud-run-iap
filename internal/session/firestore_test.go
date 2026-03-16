package session

import (
	"crypto/aes"
	"crypto/cipher"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return &Store{aead: aead}
}

func TestEncryptDecrypt(t *testing.T) {
	s := newTestStore(t)
	plaintext := []byte("super-secret-token-value")

	encrypted, err := s.encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if string(encrypted) == string(plaintext) {
		t.Error("encrypted should differ from plaintext")
	}

	decrypted, err := s.decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_DifferentCiphertexts(t *testing.T) {
	s := newTestStore(t)
	plaintext := []byte("same-token")

	enc1, _ := s.encrypt(plaintext)
	enc2, _ := s.encrypt(plaintext)

	if string(enc1) == string(enc2) {
		t.Error("two encryptions of same plaintext should differ (random nonce)")
	}

	dec1, _ := s.decrypt(enc1)
	dec2, _ := s.decrypt(enc2)
	if string(dec1) != string(dec2) {
		t.Error("both should decrypt to same value")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	s := newTestStore(t)
	_, err := s.decrypt([]byte("short"))
	if err == nil {
		t.Error("expected error for short ciphertext")
	}
}

func TestSessionToken(t *testing.T) {
	sess := &Session{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenExpiry:  time.Now().Add(time.Hour),
	}

	tok := sess.Token()
	if tok.AccessToken != "access-123" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "access-123")
	}
	if tok.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %q, want %q", tok.RefreshToken, "refresh-456")
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want %q", tok.TokenType, "Bearer")
	}
}
