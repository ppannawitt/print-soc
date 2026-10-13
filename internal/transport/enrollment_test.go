package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

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
