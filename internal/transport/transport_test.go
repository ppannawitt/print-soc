package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"socprint/internal/catalog"
)

func TestParseQueueOutput(t *testing.T) {
	checked := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
	output := "Rank   Owner   Job   Files   Size\nactive alice pstsb-sx-17 document-0123456789abcdef0123456789abcdef.pdf 1200 bytes\n1st bob psc008-18 report.pdf 900 bytes"
	snapshot := ParseQueueOutput("pstsb-sx", output, checked)
	if snapshot.KnownEmpty || !snapshot.Complete || len(snapshot.Jobs) != 2 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if snapshot.Jobs[0].Owner != "alice" || snapshot.Jobs[0].ID != "pstsb-sx-17" || snapshot.Jobs[0].FileName != "document-0123456789abcdef0123456789abcdef.pdf" {
		t.Fatalf("first queue row parsed incorrectly: %#v", snapshot.Jobs[0])
	}
	if !snapshot.CheckedAt.Equal(checked) {
		t.Fatalf("checked time = %v, want %v", snapshot.CheckedAt, checked)
	}

	empty := ParseQueueOutput("psts", "psts: no entries", checked)
	if !empty.KnownEmpty || !empty.Complete || len(empty.Jobs) != 0 {
		t.Fatalf("empty queue not recognized: %#v", empty)
	}
	unrecognized := ParseQueueOutput("psts", "printer paused: maintenance window", checked)
	if unrecognized.KnownEmpty || unrecognized.Complete || len(unrecognized.Jobs) != 0 || unrecognized.Output == "" {
		t.Fatalf("unrecognized response should be preserved: %#v", unrecognized)
	}
}

func TestQueueOwnershipRequiresUserAndOperationFilename(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	snapshot := ParseQueueOutput("psts", "active supan psts-12 document-"+id+".pdf 100 bytes", time.Now())
	if !ownsRecordedJob(snapshot, "supan", "psts-12", id) {
		t.Fatal("matching owner, job ID, and operation filename should verify")
	}
	if ownsRecordedJob(snapshot, "other", "psts-12", id) {
		t.Fatal("another user's job must not verify")
	}
	if ownsRecordedJob(snapshot, "supan", "psts-12", strings.Repeat("a", 32)) {
		t.Fatal("a recycled job ID without the matching operation filename must not verify")
	}
}

func TestShellQuoteAndSpoolerID(t *testing.T) {
	got := shellQuote("report'; touch /tmp/nope;#")
	want := "'report'\\''; touch /tmp/nope;#'"
	if got != want {
		t.Fatalf("shellQuote = %q, want %q", got, want)
	}
	output := "request id is pstsb-sx-123 (1 file)"
	match := spoolerIDPattern.FindStringSubmatch(output)
	if len(match) != 2 || match[1] != "pstsb-sx-123" {
		t.Fatalf("spooler identifier not preserved exactly: %#v", match)
	}
}

func TestRoutingOnlyFallsBackForNetworkFailure(t *testing.T) {
	if !shouldTryJump(ErrNetwork, true) {
		t.Fatal("network failure should permit jump routing")
	}
	if shouldTryJump(ErrAuthentication, true) {
		t.Fatal("authentication failure must not switch routes")
	}
	if shouldTryJump(ErrChangedHostKey, true) {
		t.Fatal("changed host key must stop route switching")
	}
	if shouldTryJump(ErrNetwork, false) {
		t.Fatal("jump routing should be disabled when requested")
	}
}

func TestHostKeyTrustAndChangeDetection(t *testing.T) {
	first, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, err := ssh.NewPublicKey(first)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := ssh.NewPublicKey(second)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	callback, err := makeHostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 22}
	host := UnixHost + ":22"
	err = callback(host, remote, firstKey)
	var trust *TrustRequired
	if !errors.As(err, &trust) || trust.Fingerprint != ssh.FingerprintSHA256(firstKey) {
		t.Fatalf("untrusted key did not request verification: %v", err)
	}
	contents := knownhosts.Line([]string{trust.Host}, firstKey) + "\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	callback, err = makeHostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := callback(host, remote, firstKey); err != nil {
		t.Fatalf("saved host key was rejected: %v", err)
	}
	if err := callback(host, remote, secondKey); !errors.Is(err, ErrChangedHostKey) {
		t.Fatalf("changed key did not stop connection: %v", err)
	}
}

func TestOpenPDFValidation(t *testing.T) {
	dir := t.TempDir()
	validPath := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(validPath, []byte("%PDF-1.7\nfixture"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openPDF(validPath)
	if err != nil {
		t.Fatalf("valid PDF rejected: %v", err)
	}
	_ = file.Close()

	invalidPath := filepath.Join(dir, "paper.txt")
	if err := os.WriteFile(invalidPath, []byte("not a PDF"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openPDF(invalidPath); err == nil {
		t.Fatal("non-PDF signature was accepted")
	}

	largePath := filepath.Join(dir, "large.pdf")
	large, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := large.Write([]byte("%PDF-")); err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(1_000_000_001); err != nil {
		t.Fatal(err)
	}
	_ = large.Close()
	if _, err := openPDF(largePath); err == nil {
		t.Fatal("PDF over 1 GB was accepted")
	}
}

func TestLocalSSHFixtureSubmitQueueAndCancel(t *testing.T) {
	remoteHome := t.TempDir()
	const operationID = "0123456789abcdef0123456789abcdef"
	const expectedQueue = "psc008-sx"
	var stateMu sync.Mutex
	queueOutput := "Rank Owner Job Files Size\nactive supan psc008-sx-42 document-" + operationID + ".pdf 20 bytes"
	lprmCalls := 0
	serverAddress, hostKey := startFixtureSSHServer(t, remoteHome, func(channel ssh.Channel, command string) {
		switch {
		case strings.HasPrefix(command, "lpq -P "):
			stateMu.Lock()
			output := queueOutput
			stateMu.Unlock()
			_, _ = io.WriteString(channel, output)
			_ = sendExitStatus(channel, 0)
		case strings.HasPrefix(command, "lprm -P "):
			stateMu.Lock()
			lprmCalls++
			stateMu.Unlock()
			_ = sendExitStatus(channel, 0)
		case strings.HasPrefix(command, "lpr -P "):
			match := regexp.MustCompile(`'([^']*document-[a-f0-9]{32}\.pdf)'`).FindStringSubmatch(command)
			if !strings.Contains(command, "'"+expectedQueue+"'") || len(match) != 2 {
				_, _ = io.WriteString(channel, "unexpected print command")
				_ = sendExitStatus(channel, 1)
				return
			}
			contents, err := os.ReadFile(match[1])
			if err != nil || !strings.HasPrefix(string(contents), "%PDF-") {
				_, _ = io.WriteString(channel, "uploaded PDF unavailable")
				_ = sendExitStatus(channel, 1)
				return
			}
			_, _ = io.WriteString(channel, "request id is psc008-sx-42 (1 file)")
			_ = sendExitStatus(channel, 0)
		default:
			_, _ = io.WriteString(channel, "unsupported fixture command")
			_ = sendExitStatus(channel, 1)
		}
	})
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	client := &SSH{username: "supan", password: "fixture-password", unixHost: serverAddress, jumpHost: "127.0.0.1:1", printers: printers, knownHosts: ssh.FixedHostKey(hostKey)}
	defer client.Close()
	pdfPath := filepath.Join(t.TempDir(), "notes-一.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\nfixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := client.Submit(ctx, PrintRequest{OperationID: operationID, FilePath: pdfPath, FileName: "notes-一.pdf", PrinterID: "psc008", Queue: expectedQueue})
	if err != nil {
		t.Fatalf("fixture submission failed: %v", err)
	}
	if result.Route != RouteDirect || result.SpoolerID != "psc008-sx-42" {
		t.Fatalf("submission metadata is wrong: %#v", result)
	}
	remoteOperation := filepath.Join(remoteHome, ".cache", "socprint", "op-"+operationID)
	if _, err := os.Stat(filepath.Join(remoteOperation, "document-"+operationID+".pdf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote PDF was not removed after submission: %v", err)
	}
	receiptBytes, err := os.ReadFile(filepath.Join(remoteOperation, "receipt.json"))
	if err != nil {
		t.Fatalf("successful submission receipt not saved: %v", err)
	}
	var receipt Receipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil || receipt.ID != operationID || receipt.SpoolerID != result.SpoolerID {
		t.Fatalf("bad recovery receipt: %#v (%v)", receipt, err)
	}

	queue, err := client.Queue(ctx, expectedQueue)
	if err != nil || !queue.Complete || queue.Route != RouteDirect || len(queue.Jobs) != 1 {
		t.Fatalf("fixture queue response is wrong: %#v (%v)", queue, err)
	}
	if queue.Jobs[0].Owner != "supan" || queue.Jobs[0].ID != result.SpoolerID {
		t.Fatalf("queue job did not parse: %#v", queue.Jobs[0])
	}

	stateMu.Lock()
	queueOutput = "Rank Owner Job Files Size\nactive another-user psc008-sx-42 document-" + operationID + ".pdf 20 bytes"
	stateMu.Unlock()
	if err := client.Cancel(ctx, expectedQueue, result.SpoolerID, operationID); err == nil {
		t.Fatal("cancellation accepted a queue entry owned by another user")
	}
	stateMu.Lock()
	if lprmCalls != 0 {
		stateMu.Unlock()
		t.Fatal("lprm ran for a queue entry owned by another user")
	}
	queueOutput = "Rank Owner Job Files Size\nactive supan psc008-sx-42 document-" + operationID + ".pdf 20 bytes"
	stateMu.Unlock()
	if err := client.Cancel(ctx, expectedQueue, result.SpoolerID, operationID); err != nil {
		t.Fatalf("cancellation of the matching owned queue entry failed: %v", err)
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	if lprmCalls != 1 {
		t.Fatalf("lprm called %d times, want 1", lprmCalls)
	}
}

func TestLocalSSHFixtureAuthenticationDoesNotFallBackToJump(t *testing.T) {
	directAddress, hostKey := startFixtureSSHServer(t, t.TempDir(), func(ssh.Channel, string) {})
	jumpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer jumpListener.Close()
	printers, _ := catalog.All()
	client := &SSH{username: "supan", password: "wrong-password", unixHost: directAddress, jumpHost: jumpListener.Addr().String(), printers: printers, knownHosts: ssh.FixedHostKey(hostKey)}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Connect(ctx, true)
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong password error = %v, want ErrAuthentication", err)
	}
	_ = jumpListener.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	connection, err := jumpListener.Accept()
	if err == nil {
		_ = connection.Close()
		t.Fatal("authentication failure incorrectly attempted jump fallback")
	}
	if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("unexpected jump listener result: %v", err)
	}
}

func TestLocalSSHFixturePasswordWorksWhenSavedKeyCannotBeRead(t *testing.T) {
	directAddress, hostKey := startFixtureSSHServer(t, t.TempDir(), func(ssh.Channel, string) {})
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	client := &SSH{
		username:   "supan",
		password:   "fixture-password",
		keyPath:    filepath.Join(t.TempDir(), "missing-private-key"),
		unixHost:   directAddress,
		jumpHost:   "127.0.0.1:1",
		printers:   printers,
		knownHosts: ssh.FixedHostKey(hostKey),
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	route, err := client.Connect(ctx, true)
	if err != nil {
		t.Fatalf("valid password login should still work when an optional key cannot be read: %v", err)
	}
	if route != RouteDirect {
		t.Fatalf("route = %v, want direct", route)
	}
}

func TestLocalSSHFixtureRejectedPrintIsKnownFailureAndCleansPDF(t *testing.T) {
	remoteHome := t.TempDir()
	const operationID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	serverAddress, hostKey := startFixtureSSHServer(t, remoteHome, func(channel ssh.Channel, command string) {
		if strings.HasPrefix(command, "lpr -P ") {
			match := regexp.MustCompile(`'([^']*document-[a-f0-9]{32}\.pdf)'`).FindStringSubmatch(command)
			if len(match) != 2 {
				_, _ = io.WriteString(channel, "no uploaded document")
				_ = sendExitStatus(channel, 1)
				return
			}
			if _, err := os.Stat(match[1]); err != nil {
				_, _ = io.WriteString(channel, "temporary document missing")
				_ = sendExitStatus(channel, 1)
				return
			}
			_, _ = io.WriteString(channel, "print quota exceeded")
			_ = sendExitStatus(channel, 1)
			return
		}
		_, _ = io.WriteString(channel, "unexpected command")
		_ = sendExitStatus(channel, 1)
	})
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	client := &SSH{username: "supan", password: "fixture-password", unixHost: serverAddress, jumpHost: "127.0.0.1:1", printers: printers, knownHosts: ssh.FixedHostKey(hostKey)}
	defer client.Close()
	pdfPath := filepath.Join(t.TempDir(), "quota.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\nfixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = client.Submit(ctx, PrintRequest{OperationID: operationID, FilePath: pdfPath, FileName: "quota.pdf", PrinterID: "psc008", Queue: "psc008-sx"})
	var submissionErr *SubmissionError
	if !errors.As(err, &submissionErr) || submissionErr.OutcomeUnknown {
		t.Fatalf("explicit server rejection should be a known failure, got %v", err)
	}
	remotePDF := filepath.Join(remoteHome, ".cache", "socprint", "op-"+operationID, "document-"+operationID+".pdf")
	if _, statErr := os.Stat(remotePDF); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected submission left remote PDF behind: %v", statErr)
	}
}

func TestLocalSSHFixtureDisconnectDuringLPRHasUnknownOutcome(t *testing.T) {
	remoteHome := t.TempDir()
	commandStarted := make(chan struct{})
	releaseCommand := make(chan struct{})
	serverAddress, hostKey := startFixtureSSHServer(t, remoteHome, func(channel ssh.Channel, command string) {
		if strings.HasPrefix(command, "lpr -P ") {
			close(commandStarted)
			<-releaseCommand
			_, _ = io.WriteString(channel, "request id is psc008-sx-44")
			_ = sendExitStatus(channel, 0)
			return
		}
		_ = sendExitStatus(channel, 1)
	})
	defer close(releaseCommand)
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	client := &SSH{username: "supan", password: "fixture-password", unixHost: serverAddress, jumpHost: "127.0.0.1:1", printers: printers, knownHosts: ssh.FixedHostKey(hostKey)}
	defer client.Close()
	pdfPath := filepath.Join(t.TempDir(), "disconnect.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\nfixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		submission Submission
		err        error
	}
	finished := make(chan result, 1)
	go func() {
		submission, err := client.Submit(ctx, PrintRequest{OperationID: "cccccccccccccccccccccccccccccccc", FilePath: pdfPath, FileName: "disconnect.pdf", PrinterID: "psc008", Queue: "psc008-sx"})
		finished <- result{submission: submission, err: err}
	}()
	select {
	case <-commandStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("print command never started in the isolated fixture")
	}
	cancel()
	select {
	case result := <-finished:
		var submissionErr *SubmissionError
		if !errors.As(result.err, &submissionErr) || !submissionErr.OutcomeUnknown {
			t.Fatalf("disconnect after lpr may have started should be unknown: %v", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("submission did not stop after context cancellation")
	}
}

func startFixtureSSHServer(t *testing.T, home string, handleCommand func(ssh.Channel, string)) (string, ssh.PublicKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	configuration := &ssh.ServerConfig{PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if metadata.User() != "supan" || string(password) != "fixture-password" {
			return nil, fmt.Errorf("fixture rejected credentials")
		}
		return nil, nil
	}}
	configuration.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go serveFixtureConnection(connection, configuration, home, handleCommand)
		}
	}()
	return listener.Addr().String(), signer.PublicKey()
}

func serveFixtureConnection(connection net.Conn, configuration *ssh.ServerConfig, home string, handleCommand func(ssh.Channel, string)) {
	serverConnection, channels, requests, err := ssh.NewServerConn(connection, configuration)
	if err != nil {
		_ = connection.Close()
		return
	}
	defer serverConnection.Close()
	go ssh.DiscardRequests(requests)
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			_ = incoming.Reject(ssh.UnknownChannelType, "session required")
			continue
		}
		channel, channelRequests, err := incoming.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for request := range channelRequests {
				switch request.Type {
				case "subsystem":
					var payload struct{ Name string }
					if ssh.Unmarshal(request.Payload, &payload) != nil || payload.Name != "sftp" {
						_ = request.Reply(false, nil)
						return
					}
					_ = request.Reply(true, nil)
					server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(home))
					if err == nil {
						_ = server.Serve()
						_ = server.Close()
					}
					return
				case "exec":
					var payload struct{ Command string }
					if ssh.Unmarshal(request.Payload, &payload) != nil {
						_ = request.Reply(false, nil)
						return
					}
					_ = request.Reply(true, nil)
					handleCommand(channel, payload.Command)
					return
				default:
					_ = request.Reply(false, nil)
				}
			}
		}()
	}
}

func sendExitStatus(channel ssh.Channel, status uint32) error {
	_, err := channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: status}))
	return err
}

func TestRemoteOutputIsBoundedDuringConcurrentCancellation(t *testing.T) {
	var output boundedOutput
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				p := []byte(strings.Repeat("x", 4096))
				n, err := output.Write(p)
				if n != len(p) || err != nil {
					t.Error("writer must consume truncated output")
				}
				_ = output.String()
			}
		}()
	}
	workers.Wait()
	value := output.String()
	if len(value) > 128*1024+100 || !strings.HasSuffix(value, "[Server output truncated]") {
		t.Fatal("untrusted output was not bounded and marked")
	}
}
