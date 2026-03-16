// Package session provides a Firestore-backed session store with AES-GCM token encryption.
package session

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/oauth2"
)

const (
	sessionCollection = "sessions"
	sessionIDBytes    = 32
	sessionTTL        = 24 * time.Hour
	gcmNonceSize      = 12
)

// Session represents a user session with decrypted tokens.
type Session struct {
	Email        string
	Name         string
	Picture      string
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// Token returns an oauth2.Token from the session.
func (s *Session) Token() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		Expiry:       s.TokenExpiry,
		TokenType:    "Bearer",
	}
}

// firestoreSession is the Firestore document structure.
type firestoreSession struct {
	Email                 string    `firestore:"email"`
	Name                  string    `firestore:"name"`
	Picture               string    `firestore:"picture"`
	EncryptedAccessToken  []byte    `firestore:"encrypted_access_token"`
	EncryptedRefreshToken []byte    `firestore:"encrypted_refresh_token"`
	TokenExpiry           time.Time `firestore:"token_expiry"`
	CreatedAt             time.Time `firestore:"created_at"`
	ExpiresAt             time.Time `firestore:"expires_at"`
}

// Store manages sessions in Firestore with AES-GCM encrypted tokens.
type Store struct {
	client *firestore.Client
	aead   cipher.AEAD
}

// NewStore creates a new Firestore-backed session store.
// encryptionKey must be exactly 32 bytes (AES-256).
func NewStore(ctx context.Context, projectID, databaseID string, encryptionKey []byte) (*Store, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("encryption key must be exactly 32 bytes, got %d", len(encryptionKey))
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, fmt.Errorf("creating firestore client: %w", err)
	}

	return &Store{client: client, aead: aead}, nil
}

// Close closes the Firestore client.
func (s *Store) Close() error {
	return s.client.Close()
}

// Create stores a new session and returns the session ID.
func (s *Store) Create(ctx context.Context, email, name, picture string, token *oauth2.Token) (string, error) {
	id, err := generateSessionID()
	if err != nil {
		return "", err
	}

	encAccess, err := s.encrypt([]byte(token.AccessToken))
	if err != nil {
		return "", fmt.Errorf("encrypting access token: %w", err)
	}

	encRefresh, err := s.encrypt([]byte(token.RefreshToken))
	if err != nil {
		return "", fmt.Errorf("encrypting refresh token: %w", err)
	}

	now := time.Now()
	doc := firestoreSession{
		Email:                 email,
		Name:                  name,
		Picture:               picture,
		EncryptedAccessToken:  encAccess,
		EncryptedRefreshToken: encRefresh,
		TokenExpiry:           token.Expiry,
		CreatedAt:             now,
		ExpiresAt:             now.Add(sessionTTL),
	}

	if _, err := s.client.Collection(sessionCollection).Doc(id).Set(ctx, doc); err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}

	return id, nil
}

// Get retrieves a session by ID. Returns nil if not found or expired.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	doc, err := s.client.Collection(sessionCollection).Doc(id).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}

	var fs firestoreSession
	if err := doc.DataTo(&fs); err != nil {
		return nil, fmt.Errorf("decoding session: %w", err)
	}

	if time.Now().After(fs.ExpiresAt) {
		s.Delete(context.Background(), id)
		return nil, nil
	}

	accessToken, err := s.decrypt(fs.EncryptedAccessToken)
	if err != nil {
		return nil, fmt.Errorf("decrypting access token: %w", err)
	}

	refreshToken, err := s.decrypt(fs.EncryptedRefreshToken)
	if err != nil {
		return nil, fmt.Errorf("decrypting refresh token: %w", err)
	}

	return &Session{
		Email:        fs.Email,
		Name:         fs.Name,
		Picture:      fs.Picture,
		AccessToken:  string(accessToken),
		RefreshToken: string(refreshToken),
		TokenExpiry:  fs.TokenExpiry,
		CreatedAt:    fs.CreatedAt,
		ExpiresAt:    fs.ExpiresAt,
	}, nil
}

// Delete removes a session.
func (s *Store) Delete(ctx context.Context, id string) {
	if _, err := s.client.Collection(sessionCollection).Doc(id).Delete(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to delete session", "error", err)
	}
}

// UpdateToken updates the encrypted tokens for a session.
func (s *Store) UpdateToken(ctx context.Context, id string, token *oauth2.Token) error {
	encAccess, err := s.encrypt([]byte(token.AccessToken))
	if err != nil {
		return fmt.Errorf("encrypting access token: %w", err)
	}

	encRefresh, err := s.encrypt([]byte(token.RefreshToken))
	if err != nil {
		return fmt.Errorf("encrypting refresh token: %w", err)
	}

	_, err = s.client.Collection(sessionCollection).Doc(id).Update(ctx, []firestore.Update{
		{Path: "encrypted_access_token", Value: encAccess},
		{Path: "encrypted_refresh_token", Value: encRefresh},
		{Path: "token_expiry", Value: token.Expiry},
	})
	return err
}

// encrypt encrypts plaintext using AES-256-GCM with a random nonce prepended.
func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt decrypts ciphertext produced by encrypt (nonce || ciphertext).
func (s *Store) decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < gcmNonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:gcmNonceSize]
	return s.aead.Open(nil, nonce, ciphertext[gcmNonceSize:], nil)
}

func generateSessionID() (string, error) {
	b := make([]byte, sessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
