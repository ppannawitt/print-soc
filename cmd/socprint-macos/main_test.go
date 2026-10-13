package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"socprint/internal/transport"

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

func TestMergeRegisteredJumpKeysPreservesFingerprintsAndIgnoresUnregisteredLocalKeys(t *testing.T) {
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
	merged, missing, err := mergeRegisteredJumpKeys(current, selected, []string{selected, oldOne}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 || len(missing) != 1 || missing[0] != fingerprint(oldTwo) {
		t.Fatalf("partial local records produced %d merged keys and missing %#v", len(merged), missing)
	}
	merged, missing, err = mergeRegisteredJumpKeys(current, selected, []string{selected, oldOne, oldTwo, extra}, nil)
	if err != nil || len(missing) != 0 || len(merged) != 3 {
		t.Fatalf("local candidates did not preserve only registered keys: keys=%d missing=%#v err=%v", len(merged), missing, err)
	}
	_, _, err = mergeRegisteredJumpKeys(current, selected, []string{selected, oldOne}, []string{extra})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatal("an explicitly pasted unregistered public key was not rejected clearly")
	}
}

func TestMergeRegisteredJumpKeysAcceptsPastedRegisteredKeys(t *testing.T) {
	selected := authorizedKeyFixture(t)
	registered := authorizedKeyFixture(t)
	fingerprint, err := fingerprintForAuthorizedKey(registered)
	if err != nil {
		t.Fatal(err)
	}
	merged, missing, err := mergeRegisteredJumpKeys([]string{fingerprint}, selected, []string{selected}, []string{registered})
	if err != nil || len(merged) != 2 || len(missing) != 0 {
		t.Fatalf("registered pasted public key was not accepted: keys=%d missing=%#v err=%v", len(merged), missing, err)
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

func TestRegisteredJumpKeyRemainsSuccessfulWhenAccessVerificationFails(t *testing.T) {
	for _, test := range []struct {
		err                  error
		status, verification string
	}{
		{nil, "ready", ""},
		{transport.ErrNetwork, "registered", "network"},
		{transport.ErrAuthentication, "registered", "accountAuthentication"},
		{transport.ErrJumpKeyRejected, "registered", "pending"},
	} {
		response := registeredJumpKeyResult("fixture", "/fixture/key", []string{"SHA256:key"}, test.err)
		if !response.OK {
			t.Fatalf("confirmed registration became a failure: %#v", response)
		}
		data := response.Data.(map[string]any)
		if data["Enrolled"] != true || data["Status"] != test.status {
			t.Fatalf("registration result = %#v", data)
		}
		if test.verification != "" && data["VerificationStatus"] != test.verification {
			t.Fatalf("verification status = %#v", data)
		}
	}
	changed := registeredJumpKeyResult("fixture", "/fixture/key", nil, transport.ErrChangedHostKey)
	if changed.OK || changed.Code != "changedHostKey" || changed.Data.(map[string]any)["Enrolled"] != true {
		t.Fatalf("host identity change must stay explicit: %#v", changed)
	}
}
