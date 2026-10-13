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

func TestMergeRegisteredJumpKeysPreservesEveryFingerprintAndRejectsExtras(t *testing.T) {
	selected := authorizedKeyFixture(t)
	oldOne := authorizedKeyFixture(t)
	oldTwo := authorizedKeyFixture(t)
	extra := authorizedKeyFixture(t)
	fingerprint := func(line string) string {
		t.Helper()
		value, err := fingerprintForAuthorizedKey(line)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	current := []string{fingerprint(oldOne), fingerprint(oldTwo)}
	merged, missing, err := mergeRegisteredJumpKeys(current, selected, []string{oldOne})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 || len(missing) != 1 || missing[0] != fingerprint(oldTwo) {
		t.Fatalf("partial local records produced %d merged keys and missing %#v", len(merged), missing)
	}
	merged, missing, err = mergeRegisteredJumpKeys(current, selected, []string{oldOne, oldTwo})
	if err != nil || len(missing) != 0 || len(merged) != 3 {
		t.Fatalf("complete records did not preserve the list: keys=%d missing=%#v err=%v", len(merged), missing, err)
	}
	if _, _, err := mergeRegisteredJumpKeys(current, selected, []string{oldOne, oldTwo, extra}); err == nil {
		t.Fatal("an unregistered, unselected public key was accepted")
	}
}

func authorizedKeyFixture(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}
