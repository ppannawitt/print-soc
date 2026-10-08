package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestPublicKeyForManualRegistrationFromEncryptedAndPlainPrivateKeys(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, encrypted := range []bool{false, true} {
		name := "plain"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			block, err := ssh.MarshalPrivateKey(private, "fixture")
			if encrypted {
				block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte("fixture-passphrase"))
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
				t.Fatal(err)
			}
			passphrase := ""
			if encrypted {
				passphrase = "fixture-passphrase"
			}
			got, err := publicKeyFromPrivateFile(path, passphrase)
			if err != nil {
				t.Fatal(err)
			}
			if got != strings.TrimSpace(string(ssh.MarshalAuthorizedKey(expected))) {
				t.Fatal("export returned a different public key")
			}
			if encrypted {
				for _, wrong := range []string{"", "wrong-passphrase"} {
					if _, err := publicKeyFromPrivateFile(path, wrong); err == nil {
						t.Fatal("encrypted key accepted missing or wrong passphrase")
					}
				}
			}
		})
	}
}

func TestKeyUnlockErrorsDistinguishLocalKeyAndAccountAuthentication(t *testing.T) {
	if message := keyUnlockError(&ssh.PassphraseMissingError{}); !strings.Contains(message, "key passphrase") || !strings.Contains(message, "SoC password") {
		t.Fatal(message)
	}
	if message := keyUnlockError(os.ErrNotExist); !strings.Contains(message, "file") || strings.Contains(message, "password") {
		t.Fatal(message)
	}
}
