package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedCredentialStorageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	first := []byte("fake-session-with-long-cookie=" + strings.Repeat("A", 3000))
	next := []byte("updated-cookie=" + strings.Repeat("B", 3000))
	for _, want := range [][]byte{first, next} {
		if err := writeStoredCredentials(dir, want); err != nil {
			t.Fatalf("write credentials: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, encryptedCredentialsFile))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, want) {
			t.Fatal("credential file contains plaintext credentials")
		}
		got, err := readStoredCredentials(dir)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("stored credential round trip failed: %v", err)
		}
	}
	if err := removeStoredCredentials(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := readStoredCredentials(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("read after sign-out: %v, want file not found", err)
	}
}
