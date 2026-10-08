package transport

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestPrivateKeySignerSupportsEncryptedAndPlainKeys(t *testing.T) {
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]any{"ed25519": ed, "rsa": rsaKey, "ecdsa": ec} {
		for _, encrypted := range []bool{false, true} {
			name := name
			if encrypted {
				name += "-encrypted"
			}
			t.Run(name, func(t *testing.T) {
				block, err := ssh.MarshalPrivateKey(key, "fixture")
				if encrypted {
					block, err = ssh.MarshalPrivateKeyWithPassphrase(key, "fixture", []byte("fixture-passphrase"))
				}
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "key")
				if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
					t.Fatal(err)
				}
				// Leftover saved passphrases must not break an unencrypted key.
				passphrase := "leftover-passphrase"
				if encrypted {
					passphrase = "fixture-passphrase"
				}
				signer, err := LoadPrivateKey(path, passphrase)
				if err != nil {
					t.Fatal(err)
				}
				data := []byte("private-key-fixture")
				signature, err := signer.Sign(rand.Reader, data)
				if err != nil {
					t.Fatal(err)
				}
				if err := signer.PublicKey().Verify(data, signature); err != nil {
					t.Fatal(err)
				}
				if encrypted {
					if _, err := LoadPrivateKey(path, ""); err == nil {
						t.Fatal("missing passphrase accepted")
					} else {
						var missing *ssh.PassphraseMissingError
						if !errors.As(err, &missing) {
							t.Fatal(err)
						}
					}
					if _, err := LoadPrivateKey(path, "wrong"); err == nil {
						t.Fatal("wrong passphrase accepted")
					}
				}
			})
		}
	}
}

func TestPrivateKeyFileRejectsMalformedAndOversizedInputs(t *testing.T) {
	for _, value := range []string{"not a key", strings.Repeat("x", (1<<20)+1)} {
		path := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPrivateKey(path, ""); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	if _, err := LoadPrivateKey(t.TempDir(), ""); err == nil {
		t.Fatal("directory accepted")
	}
}

func TestKeyOnlySSHAuthenticationUsesParsedSigner(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ssh.NewSignerFromKey(private)
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
			passphrase := ""
			if encrypted {
				passphrase = "fixture-passphrase"
				block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte(passphrase))
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			server := &ssh.ServerConfig{PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
				if meta.User() != "fixture" || ssh.FingerprintSHA256(key) != ssh.FingerprintSHA256(expected.PublicKey()) {
					return nil, errors.New("unexpected fixture key")
				}
				return nil, nil
			}}
			server.AddHostKey(expected)
			finished := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					finished <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				session, _, _, err := ssh.NewServerConn(conn, server)
				if session != nil {
					defer session.Close()
				}
				finished <- err
			}()
			clientState := &SSH{username: "fixture", keyPath: path, keyPass: passphrase, knownHosts: ssh.FixedHostKey(expected.PublicKey())}
			config, err := clientState.clientConfig(JumpHost)
			if err != nil {
				t.Fatal(err)
			}
			client, err := ssh.Dial("tcp", listener.Addr().String(), config)
			if err != nil {
				t.Fatal(err)
			}
			_ = client.Close()
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
		})
	}
}
