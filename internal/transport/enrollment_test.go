package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseJumpKeyConfigOnlyReadsJumpHostFingerprints(t *testing.T) {
	output := `Welcome to SoC SSH Key and Jump Host Config Server.
*** SSH keys configured for Jump Host:
256 SHA256:jumpKey (ED25519)
256 SHA256:secondKey (ED25519)
*** SSH keys configured for local servers:
256 SHA256:localKey (ED25519)
`
	got, err := ParseJumpKeyConfig(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fingerprints) != 2 || got.Fingerprints[0] != "SHA256:jumpKey" || got.Fingerprints[1] != "SHA256:secondKey" {
		t.Fatalf("jump fingerprints = %#v", got.Fingerprints)
	}
}

func TestParseJumpKeyConfigRecognizesEmptyAndRejectsUnknownOutput(t *testing.T) {
	got, err := ParseJumpKeyConfig("*** No SSH keys configured for Jump Host.\n*** No SSH keys configured for local servers.\n")
	if err != nil || len(got.Fingerprints) != 0 {
		t.Fatalf("empty config = %#v, %v", got, err)
	}
	if _, err := ParseJumpKeyConfig("*** SSH keys configured for local servers:\n"); err == nil {
		t.Fatal("local-server section accepted as a jump-key configuration")
	}
}

func TestMergeAuthorizedKeysDeduplicatesAndRejectsExtraKeysOnLine(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	_, _, err = MergeAuthorizedKeys([]string{line + "\n" + line})
	if err == nil {
		t.Fatal("multiple keys on one line accepted")
	}
	merged, fingerprints, err := MergeAuthorizedKeys([]string{line, "  " + line + " "})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || len(fingerprints) != 1 {
		t.Fatalf("merge result = %d lines, %d fingerprints", len(merged), len(fingerprints))
	}
}

func TestVerifyJumpEnrollmentRequiresServiceConfirmationAndAllFingerprints(t *testing.T) {
	required := map[string]struct{}{"SHA256:new": {}, "SHA256:old": {}}
	updated := JumpKeyConfig{Fingerprints: []string{"SHA256:new", "SHA256:old"}}
	if err := verifyJumpEnrollment("OK: Authorised keys updated for jump access.\n", nil, updated, required); err != nil {
		t.Fatalf("confirmed complete enrollment: %v", err)
	}
	if err := verifyJumpEnrollment("OK: Authorised keys updated for jump access.\n", nil, JumpKeyConfig{Fingerprints: []string{"SHA256:new"}}, required); !errors.Is(err, ErrJumpEnrollmentUncertain) {
		t.Fatalf("missing preserved key should be uncertain, got %v", err)
	}
	if err := verifyJumpEnrollment("", nil, updated, required); !errors.Is(err, ErrJumpEnrollmentUncertain) {
		t.Fatalf("missing response should be uncertain, got %v", err)
	}
}

func TestVerifyJumpEnrollmentHandlesRejectionAndInterruptedResponse(t *testing.T) {
	required := map[string]struct{}{"SHA256:new": {}}
	if err := verifyJumpEnrollment("error: Authorised keys are invalid.\n", errors.New("remote exit"), JumpKeyConfig{}, required); !errors.Is(err, ErrJumpEnrollmentRejected) {
		t.Fatalf("explicit service rejection should be classified, got %v", err)
	}
	if err := verifyJumpEnrollment("", errors.New("connection lost"), JumpKeyConfig{}, required); !errors.Is(err, ErrJumpEnrollmentUncertain) {
		t.Fatalf("interrupted registration should be uncertain, got %v", err)
	}
	updated := JumpKeyConfig{Fingerprints: []string{"SHA256:new"}}
	if err := verifyJumpEnrollment("Authorised keys updated for jump access", errors.New("connection lost after output"), updated, required); err != nil {
		t.Fatalf("fingerprints and success output verify an interrupted response: %v", err)
	}
}

func TestJumpKeyCheckRetriesOtherResolvedServersAfterOneRejects(t *testing.T) {
	clientKey := testSigner(t)
	hostKey := testSigner(t)
	accepted := ssh.FingerprintSHA256(clientKey.PublicKey())
	deniedAddress := startPublicKeyServer(t, hostKey, "")
	acceptedAddress := startPublicKeyServer(t, hostKey, accepted)
	config := &ssh.ClientConfig{
		User: "fixture", Auth: []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()), Timeout: time.Second,
	}
	client, err := dialJumpKeyAddresses(context.Background(), "sjump.comp.nus.edu.sg:22", []string{deniedAddress, acceptedAddress}, config)
	if err != nil {
		t.Fatalf("a later jump server accepts the key: %v", err)
	}
	_ = client.Close()
}

func TestJumpKeyCheckDoesNotCallMixedAuthAndNetworkFailuresARejectedKey(t *testing.T) {
	clientKey := testSigner(t)
	hostKey := testSigner(t)
	deniedAddress := startPublicKeyServer(t, hostKey, "")
	config := &ssh.ClientConfig{
		User: "fixture", Auth: []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()), Timeout: time.Second,
	}
	_, err := dialJumpKeyAddresses(context.Background(), "sjump.comp.nus.edu.sg:22", []string{deniedAddress, "127.0.0.1:1"}, config)
	if !errors.Is(err, ErrNetwork) || errors.Is(err, ErrJumpKeyRejected) {
		t.Fatalf("mixed reachability must be reported as a network uncertainty, got %v", err)
	}
}

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestJumpKeyAuthenticationRequiresKeyAcceptanceBeforePassword(t *testing.T) {
	for _, test := range []struct {
		name                                      string
		acceptKey, requirePassword, offerPassword bool
		password                                  string
		wantErr                                   error
		wantPasswordAttempts                      int32
	}{
		{"key alone", true, false, false, "fixture-password", nil, 0},
		{"key then password", true, true, false, "fixture-password", nil, 1},
		{"accepted key wrong password", true, true, false, "wrong-password", ErrAuthentication, 1},
		{"rejected key with password fallback", false, false, true, "fixture-password", ErrJumpKeyRejected, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			key, hostKey := testSigner(t), testSigner(t)
			var passwordAttempts atomic.Int32
			passwordCallback := func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
				passwordAttempts.Add(1)
				if string(password) != "fixture-password" {
					return nil, errors.New("incorrect password")
				}
				return nil, nil
			}
			server := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, offered ssh.PublicKey) (*ssh.Permissions, error) {
				if test.acceptKey && ssh.FingerprintSHA256(offered) == ssh.FingerprintSHA256(key.PublicKey()) {
					return nil, nil
				}
				return nil, errors.New("key rejected")
			}}
			if test.requirePassword {
				server.VerifiedPublicKeyCallback = func(_ ssh.ConnMetadata, _ ssh.PublicKey, _ *ssh.Permissions, _ string) (*ssh.Permissions, error) {
					return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{PasswordCallback: passwordCallback}}
				}
			}
			if test.offerPassword {
				server.PasswordCallback = passwordCallback
			}
			address := startAuthenticationServer(t, hostKey, server, nil)
			config := jumpKeyClientConfig("supan", test.password, key, ssh.FixedHostKey(hostKey.PublicKey()))
			client, err := dialJumpKeyAddresses(context.Background(), address, []string{address}, config)
			if client != nil {
				_ = client.Close()
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("authentication error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == ErrAuthentication && errors.Is(err, ErrJumpKeyRejected) {
				t.Fatal("accepted key reported as rejected")
			}
			if got := passwordAttempts.Load(); got != test.wantPasswordAttempts {
				t.Fatalf("password attempts = %d, want %d", got, test.wantPasswordAttempts)
			}
		})
	}
}

func TestCheckJumpKeyVerifiesDestinationAfterKeyAndPassword(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte("fixture-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	targetAddress, targetKey := startFixtureSSHServer(t, t.TempDir(), func(ssh.Channel, string) {})
	jumpKey := testSigner(t)
	server := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if ssh.FingerprintSHA256(key) != ssh.FingerprintSHA256(signer.PublicKey()) {
				return nil, errors.New("wrong key")
			}
			return nil, nil
		},
		VerifiedPublicKeyCallback: func(_ ssh.ConnMetadata, _ ssh.PublicKey, _ *ssh.Permissions, _ string) (*ssh.Permissions, error) {
			return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
				if string(password) != "fixture-password" {
					return nil, errors.New("wrong password")
				}
				return nil, nil
			}}}
		},
	}
	jumpAddress := startAuthenticationServer(t, jumpKey, server, func(incoming ssh.NewChannel) {
		var request struct {
			Host       string
			Port       uint32
			Origin     string
			OriginPort uint32
		}
		if incoming.ChannelType() != "direct-tcpip" || ssh.Unmarshal(incoming.ExtraData(), &request) != nil || net.JoinHostPort(request.Host, fmt.Sprint(request.Port)) != targetAddress {
			_ = incoming.Reject(ssh.Prohibited, "unexpected destination")
			return
		}
		connection, err := net.Dial("tcp", targetAddress)
		if err != nil {
			_ = incoming.Reject(ssh.ConnectionFailed, "fixture unavailable")
			return
		}
		channel, requests, err := incoming.Accept()
		if err != nil {
			_ = connection.Close()
			return
		}
		defer channel.Close()
		defer connection.Close()
		go ssh.DiscardRequests(requests)
		go func() { _, _ = io.Copy(connection, channel); _ = connection.Close() }()
		_, _ = io.Copy(channel, connection)
	})
	client := &SSH{username: "supan", password: "fixture-password", keyPath: keyPath, keyPass: "fixture-passphrase", jumpHost: jumpAddress, unixHost: targetAddress,
		knownHosts: func(host string, _ net.Addr, key ssh.PublicKey) error {
			expected := targetKey
			if host == jumpAddress {
				expected = jumpKey.PublicKey()
			}
			if ssh.FingerprintSHA256(key) != ssh.FingerprintSHA256(expected) {
				return ErrChangedHostKey
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.CheckJumpKey(ctx); err != nil {
		t.Fatalf("key + password + destination verification failed: %v", err)
	}
}

func startPublicKeyServer(t *testing.T, hostKey ssh.Signer, acceptedFingerprint string) string {
	t.Helper()
	server := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if acceptedFingerprint != "" && ssh.FingerprintSHA256(key) == acceptedFingerprint {
			return nil, nil
		}
		return nil, errors.New("public key not authorized")
	}}
	return startAuthenticationServer(t, hostKey, server, nil)
}

func startAuthenticationServer(t *testing.T, hostKey ssh.Signer, server *ssh.ServerConfig, handleChannel func(ssh.NewChannel)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.AddHostKey(hostKey)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, channels, requests, handshakeErr := ssh.NewServerConn(connection, server)
				if handshakeErr != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if handleChannel != nil {
						go handleChannel(channel)
					} else {
						_ = channel.Reject(ssh.Prohibited, "not used in this fixture")
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}
