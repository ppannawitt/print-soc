package transport

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"socprint/internal/catalog"
	"socprint/internal/config"
	"socprint/internal/credentials"
)

const (
	UnixHost = "stu.comp.nus.edu.sg"
	JumpHost = "sjump.comp.nus.edu.sg"
)

var (
	ErrAuthentication = errors.New("SoC account authentication failed")
	ErrNetwork        = errors.New("could not connect to the SoC Unix server")
	ErrChangedHostKey = errors.New("the saved SSH host key changed; connection stopped")
	ErrNoKey          = errors.New("jump access needs an enrolled SoC SSH key")
	ErrKeyEnrollment  = errors.New("the SoC jump host rejected the SSH key; check that its public key is enrolled")
	ErrKeyPassphrase  = errors.New("the selected SSH key could not be read or unlocked")
)

type TrustRequired struct {
	Host        string
	Fingerprint string
	Key         ssh.PublicKey
}

func (e *TrustRequired) Error() string {
	return fmt.Sprintf("first connection to %s presents host-key fingerprint %s", e.Host, e.Fingerprint)
}

type Route int

const (
	RouteUnknown Route = iota
	RouteDirect
	RouteJump
)

func (r Route) String() string {
	switch r {
	case RouteDirect:
		return "Direct"
	case RouteJump:
		return "Jump host"
	default:
		return "Unknown"
	}
}

type ProbeResult struct {
	Route Route
	Err   error
}

type PrintRequest struct {
	OperationID string
	FilePath    string
	FileName    string
	PrinterID   string
	Queue       string
	Copies      int
}

type Submission struct {
	OperationID string
	SpoolerID   string
	Output      string
	SubmittedAt time.Time
	Route       Route
}

type SubmissionError struct {
	OutcomeUnknown bool
	Err            error
}

func (e *SubmissionError) Error() string { return e.Err.Error() }
func (e *SubmissionError) Unwrap() error { return e.Err }

type CommandStartError struct{ Err error }

func (e *CommandStartError) Error() string {
	return "remote print command did not start: " + e.Err.Error()
}
func (e *CommandStartError) Unwrap() error { return e.Err }

type QueueSnapshot struct {
	Queue      string
	Output     string
	CheckedAt  time.Time
	KnownEmpty bool
	Complete   bool
	Route      Route
	Jobs       []QueueJob
}

type Receipt struct {
	ID        string    `json:"id"`
	Queue     string    `json:"queue"`
	Printer   string    `json:"printer"`
	File      string    `json:"file"`
	Submitted time.Time `json:"submitted"`
	SpoolerID string    `json:"spooler_id,omitempty"`
}

type QueueJob struct {
	Rank     string
	Owner    string
	ID       string
	FileName string
	Size     string
	Raw      string
}

// Client deliberately exposes only the operations the interactive app needs.
type Client interface {
	Detect(context.Context) ProbeResult
	Connect(context.Context, bool) (Route, error)
	Trust(*TrustRequired) error
	Submit(context.Context, PrintRequest) (Submission, error)
	Queue(context.Context, string) (QueueSnapshot, error)
	Cancel(context.Context, string, string, string) error
	Recover(context.Context, []string) (map[string]Receipt, error)
	Close() error
}

type SSH struct {
	mu         sync.Mutex
	username   string
	password   string
	keyPath    string
	keyPass    string
	unixHost   string
	jumpHost   string
	hostsPath  string
	printers   []catalog.Printer
	lastRoute  Route
	closed     bool
	keyError   error
	knownHosts ssh.HostKeyCallback
}

func New(username, password, keyPath string, printers []catalog.Printer, sessionPassphrase ...string) (*SSH, error) {
	directory, err := config.Directory()
	if err != nil {
		return nil, err
	}
	hostsPath := filepath.Join(directory, "known_hosts")
	if _, err := os.Stat(hostsPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(hostsPath, nil, 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if err := os.Chmod(hostsPath, 0600); err != nil {
		return nil, err
	}
	callback, err := makeHostKeyCallback(hostsPath)
	if err != nil {
		return nil, err
	}
	passphrase := ""
	if len(sessionPassphrase) > 0 {
		passphrase = sessionPassphrase[0]
	}
	if passphrase == "" && username != "" {
		passphrase, _ = credentials.GetPassphrase(username)
	}
	return &SSH{username: username, password: password, keyPath: keyPath, keyPass: passphrase, unixHost: UnixHost, jumpHost: JumpHost, hostsPath: hostsPath, printers: printers, knownHosts: callback}, nil
}

func hostAddress(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, "22")
}

func makeHostKeyCallback(path string) (ssh.HostKeyCallback, error) {
	checker, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := checker(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				return &TrustRequired{Host: hostname, Fingerprint: ssh.FingerprintSHA256(key), Key: key}
			}
			return ErrChangedHostKey
		}
		return err
	}, nil
}

func (s *SSH) Trust(required *TrustRequired) error {
	if required == nil || required.Key == nil || required.Fingerprint != ssh.FingerprintSHA256(required.Key) {
		return errors.New("invalid host key confirmation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.hostsPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintln(file, knownhosts.Line([]string{required.Host}, required.Key))
	if err != nil {
		return err
	}
	s.knownHosts, err = makeHostKeyCallback(s.hostsPath)
	return err
}

func (s *SSH) authMethods() ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	s.keyError = nil
	if s.keyPath != "" {
		signer, err := LoadPrivateKey(s.keyPath, s.keyPass)
		if err != nil {
			s.keyError = fmt.Errorf("%w: %v", ErrKeyPassphrase, err)
		} else {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}
	if s.password != "" {
		methods = append(methods, ssh.Password(s.password))
	}
	if len(methods) == 0 {
		if s.keyError != nil {
			return nil, s.keyError
		}
		return nil, ErrAuthentication
	}
	return methods, nil
}

func (s *SSH) clientConfig(host string) (*ssh.ClientConfig, error) {
	auth, err := s.authMethods()
	if err != nil {
		return nil, err
	}
	callback := s.knownHosts
	return &ssh.ClientConfig{User: s.username, Auth: auth, HostKeyCallback: callback, Timeout: 8 * time.Second}, nil
}

func (s *SSH) dialDirect(ctx context.Context) (*ssh.Client, error) {
	address := hostAddress(s.unixHost)
	connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancel()
	config, err := s.clientConfig(s.unixHost)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return s.completeHandshake(connection, address, config)
}

func (s *SSH) dialJump(ctx context.Context) (*ssh.Client, *ssh.Client, error) {
	address := hostAddress(s.jumpHost)
	connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("%w through the jump host: %v", ErrNetwork, err)
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancel()
	config, err := s.clientConfig(s.jumpHost)
	if err != nil {
		_ = connection.Close()
		return nil, nil, err
	}
	jump, err := s.completeHandshake(connection, address, config)
	if err != nil {
		if errors.Is(err, ErrAuthentication) {
			if s.keyPath == "" {
				return nil, nil, ErrNoKey
			}
			if s.keyError != nil {
				return nil, nil, s.keyError
			}
			return nil, nil, fmt.Errorf("%w: %v", ErrKeyEnrollment, err)
		}
		return nil, nil, err
	}
	target, err := jump.Dial("tcp", hostAddress(s.unixHost))
	if err != nil {
		_ = jump.Close()
		return nil, nil, fmt.Errorf("jump host could not reach %s: %w", s.unixHost, err)
	}
	targetConfig, err := s.clientConfig(s.unixHost)
	if err != nil {
		_ = target.Close()
		_ = jump.Close()
		return nil, nil, err
	}
	client, err := s.completeHandshake(target, hostAddress(s.unixHost), targetConfig)
	if err != nil {
		_ = jump.Close()
		return nil, nil, err
	}
	return client, jump, nil
}

func (s *SSH) completeHandshake(connection net.Conn, address string, clientConfig *ssh.ClientConfig) (*ssh.Client, error) {
	_ = connection.SetDeadline(time.Now().Add(9 * time.Second))
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, clientConfig)
	if err != nil {
		_ = connection.Close()
		var trust *TrustRequired
		if errors.As(err, &trust) {
			return nil, trust
		}
		if errors.Is(err, ErrChangedHostKey) {
			return nil, ErrChangedHostKey
		}
		if strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") {
			return nil, ErrAuthentication
		}
		return nil, fmt.Errorf("SSH handshake to %s failed: %w", address, err)
	}
	_ = connection.SetDeadline(time.Time{})
	return ssh.NewClient(clientConnection, channels, requests), nil
}

func (s *SSH) Detect(ctx context.Context) ProbeResult {
	return detectHosts(ctx, s.unixHost, s.jumpHost)
}

func DetectNetwork(ctx context.Context) ProbeResult {
	return detectHosts(ctx, UnixHost, JumpHost)
}

func detectHosts(ctx context.Context, unixHost, jumpHost string) ProbeResult {
	for _, route := range []Route{RouteDirect, RouteJump} {
		host := unixHost
		if route == RouteJump {
			host = jumpHost
		}
		connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", hostAddress(host))
		if err == nil {
			_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
			banner, readErr := bufio.NewReader(connection).ReadString('\n')
			_ = connection.Close()
			if readErr == nil && (strings.HasPrefix(banner, "SSH-2.0-") || strings.HasPrefix(banner, "SSH-1.99-")) {
				return ProbeResult{Route: route}
			}
		}
	}
	return ProbeResult{Err: ErrNetwork}
}

func (s *SSH) Connect(ctx context.Context, allowJump bool) (Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RouteUnknown, errors.New("connection manager is closed")
	}
	client, err := s.dialDirect(ctx)
	if err == nil {
		_ = client.Close()
		s.lastRoute = RouteDirect
		return RouteDirect, nil
	}
	if !shouldTryJump(err, allowJump) {
		return RouteUnknown, err
	}
	client, parent, jumpErr := s.dialJump(ctx)
	if jumpErr == nil {
		_ = client.Close()
		_ = parent.Close()
		s.lastRoute = RouteJump
		return RouteJump, nil
	}
	return RouteUnknown, jumpErr
}

func isTrustOrAuthError(err error) bool {
	return errors.Is(err, ErrAuthentication) || errors.Is(err, ErrChangedHostKey) || errors.Is(err, ErrNoKey) || errors.As(err, new(*TrustRequired))
}

func shouldTryJump(err error, allowJump bool) bool {
	return allowJump && errors.Is(err, ErrNetwork) && !isTrustOrAuthError(err)
}

func (s *SSH) connected(ctx context.Context) (*ssh.Client, *ssh.Client, Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, RouteUnknown, errors.New("connection manager is closed")
	}
	client, err := s.dialDirect(ctx)
	if err == nil {
		s.lastRoute = RouteDirect
		return client, nil, RouteDirect, nil
	}
	if !shouldTryJump(err, true) {
		return nil, nil, RouteUnknown, err
	}
	client, parent, err := s.dialJump(ctx)
	if err != nil {
		return nil, nil, RouteUnknown, err
	}
	s.lastRoute = RouteJump
	return client, parent, RouteJump, nil
}

func closeClients(client, parent *ssh.Client) {
	if client != nil {
		_ = client.Close()
	}
	if parent != nil {
		_ = parent.Close()
	}
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

var spoolerIDPattern = regexp.MustCompile(`(?i)(?:request\s+id\s+is|job(?:\s+id)?\s*[:#]?)\s*([a-z0-9][a-z0-9_-]{0,79})\b`)

func (s *SSH) Submit(ctx context.Context, request PrintRequest) (submission Submission, submitErr error) {
	printer, ok := catalog.Find(s.printers, request.PrinterID)
	if !ok || catalog.ValidateQueue(s.printers, request.PrinterID, request.Queue, true) != nil {
		return Submission{}, errors.New("the selected queue is not an allowed student queue")
	}
	if request.Copies == 0 {
		request.Copies = 1
	}
	if request.Copies < 1 || request.Copies > 99 {
		return Submission{}, errors.New("copies must be between 1 and 99")
	}
	_ = printer
	file, err := openPDF(request.FilePath)
	if err != nil {
		return Submission{}, err
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		return Submission{}, err
	}
	if request.OperationID == "" {
		request.OperationID, err = randomID()
		if err != nil {
			return Submission{}, err
		}
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(request.OperationID) {
		return Submission{}, errors.New("invalid local print operation identifier")
	}
	client, parent, route, err := s.connected(ctx)
	if err != nil {
		return Submission{}, err
	}
	defer closeClients(client, parent)
	stopCancel := context.AfterFunc(ctx, func() { closeClients(client, parent) })
	defer stopCancel()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return Submission{}, fmt.Errorf("could not open secure file transfer: %w", err)
	}
	defer sftpClient.Close()
	home, err := sftpClient.Getwd()
	if err != nil || home == "" || !strings.HasPrefix(home, "/") {
		return Submission{}, errors.New("the server did not provide a safe absolute home directory for temporary print files")
	}
	root := pathJoin(home, ".cache", "socprint")
	if err := sftpClient.MkdirAll(root); err != nil {
		return Submission{}, fmt.Errorf("could not create a private temporary directory on stu: %w", err)
	}
	if err := sftpClient.Chmod(root, 0700); err != nil {
		return Submission{}, fmt.Errorf("could not protect the temporary print directory: %w", err)
	}
	operation := pathJoin(root, "op-"+request.OperationID)
	if err := sftpClient.Mkdir(operation); err != nil {
		return Submission{}, fmt.Errorf("could not reserve this print operation on the server: %w", err)
	}
	remoteFile := pathJoin(operation, "document-"+request.OperationID+".pdf")
	cleanupPerformed := false
	defer func() {
		if !cleanupPerformed {
			removeErr := sftpClient.Remove(remoteFile)
			directoryErr := sftpClient.RemoveDirectory(operation)
			if removeErr != nil && !os.IsNotExist(removeErr) || directoryErr != nil && !os.IsNotExist(directoryErr) {
				submission.Output += "\nThe server could not confirm cleanup of the temporary PDF or its directory."
				if submitErr != nil {
					submitErr = fmt.Errorf("%w; temporary upload cleanup could not be confirmed", submitErr)
				}
			}
		}
	}()
	if err := sftpClient.Chmod(operation, 0700); err != nil {
		return Submission{}, err
	}

	upload, err := sftpClient.OpenFile(remoteFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return Submission{}, fmt.Errorf("could not create the remote temporary PDF: %w", err)
	}
	if err := upload.Chmod(0600); err != nil {
		_ = upload.Close()
		return Submission{}, err
	}
	bytesUploaded, err := io.Copy(upload, io.LimitReader(file, 1_000_000_001))
	if err == nil && (bytesUploaded > 1_000_000_000 || bytesUploaded != fileInfo.Size()) {
		err = errors.New("PDF changed while it was being uploaded; select it again and retry")
	}
	if err != nil {
		_ = upload.Close()
		_ = sftpClient.Remove(remoteFile)
		return Submission{}, fmt.Errorf("PDF upload failed: %w", err)
	}
	if err := upload.Close(); err != nil {
		_ = sftpClient.Remove(remoteFile)
		return Submission{}, fmt.Errorf("PDF upload did not finish: %w", err)
	}
	jobTitle := strings.TrimSpace(filepath.Base(request.FileName))
	jobTitle = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, jobTitle)
	if jobTitle == "" || jobTitle == "." || jobTitle == string(filepath.Separator) {
		jobTitle = "document.pdf"
	}
	command := "lpr -P " + shellQuote(request.Queue) + " -J " + shellQuote(jobTitle)
	if request.Copies > 1 {
		command += " -# " + strconv.Itoa(request.Copies)
	}
	command += " " + shellQuote(remoteFile)
	output, err := runRemote(ctx, client, command)
	cleanupErr := sftpClient.Remove(remoteFile)
	cleanupPerformed = true
	if err != nil {
		result := Submission{OperationID: request.OperationID, Output: safeOutput(output), SubmittedAt: time.Now().UTC(), Route: route}
		if cleanupErr != nil {
			result.Output += "\nThe server could not confirm cleanup of the temporary PDF."
		}
		var exit *ssh.ExitError
		var startErr *CommandStartError
		unknown := !errors.As(err, &exit) && !errors.As(err, &startErr)
		message := fmt.Errorf("print command did not confirm success: %w: %s", err, result.Output)
		return result, &SubmissionError{OutcomeUnknown: unknown, Err: message}
	}
	result := Submission{OperationID: request.OperationID, Output: safeOutput(output), SubmittedAt: time.Now().UTC(), Route: route}
	if match := spoolerIDPattern.FindStringSubmatch(output); len(match) == 2 {
		result.SpoolerID = match[1]
	}
	receipt := Receipt{result.OperationID, request.Queue, printer.ID, request.FileName, result.SubmittedAt, result.SpoolerID}
	receiptBytes, _ := json.Marshal(receipt)
	if err := writeRemoteReceipt(sftpClient, operation, receiptBytes); err != nil {
		// lpr already returned success; the local record remains submitted and is never resent.
		result.Output += "\nThe server accepted the job, but its recovery receipt could not be saved."
	}
	if cleanupErr != nil {
		result.Output += "\nThe server accepted the job, but the temporary PDF could not be removed."
	}
	return result, nil
}

func openPDF(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not read selected PDF: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1_000_000_000 {
		_ = file.Close()
		return nil, errors.New("select a nonempty PDF no larger than the 1 GB SoC print-job limit")
	}
	var signature [5]byte
	if _, err := io.ReadFull(file, signature[:]); err != nil || string(signature[:]) != "%PDF-" {
		_ = file.Close()
		return nil, errors.New("the selected file does not have a PDF signature")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func pathJoin(elements ...string) string { return filepath.ToSlash(filepath.Join(elements...)) }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func runRemote(ctx context.Context, client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", &CommandStartError{Err: err}
	}
	defer session.Close()
	var stdout, stderr boundedOutput
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case err := <-done:
		return stdout.String() + stderr.String(), err
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
		return stdout.String() + stderr.String(), ctx.Err()
	}
}

// SSH output can arrive concurrently with cancellation. Bound untrusted server
// output and synchronize reads with the SSH copy goroutines.
type boundedOutput struct {
	mu        sync.Mutex
	text      strings.Builder
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := len(p)
	remaining := 128*1024 - b.text.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.text.Write(p)
	return count, nil
}
func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.truncated {
		return b.text.String() + "\n[Server output truncated]"
	}
	return b.text.String()
}

func safeOutput(value string) string {
	var cleaned strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || character >= 0x20 && character != 0x7f && (character < 0x80 || character > 0x9f) {
			cleaned.WriteRune(character)
		}
	}
	return strings.TrimSpace(cleaned.String())
}

func writeRemoteReceipt(sftpClient *sftp.Client, directory string, contents []byte) error {
	temporary := pathJoin(directory, "receipt.tmp")
	final := pathJoin(directory, "receipt.json")
	file, err := sftpClient.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return sftpClient.Rename(temporary, final)
}

func ParseQueueOutput(queue, output string, checkedAt time.Time) QueueSnapshot {
	output = safeOutput(output)
	snapshot := QueueSnapshot{Queue: queue, Output: output, CheckedAt: checkedAt, KnownEmpty: strings.Contains(strings.ToLower(output), "no entries") || strings.Contains(strings.ToLower(output), "queue is empty")}
	if snapshot.KnownEmpty {
		snapshot.Complete = true
		return snapshot
	}
	headerSeen, onlyRecognizedRows := false, true
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "rank") && strings.Contains(lower, "owner") && strings.Contains(lower, "job") {
			headerSeen = true
			continue
		}
		if strings.HasPrefix(lower, "printer:") {
			continue
		}
		if len(fields) < 4 {
			onlyRecognizedRows = false
			continue
		}
		if !(strings.EqualFold(fields[0], "active") || strings.HasSuffix(fields[0], "th") || fields[0] == "1st" || fields[0] == "2nd" || fields[0] == "3rd") {
			onlyRecognizedRows = false
			continue
		}
		job := QueueJob{Rank: fields[0], Owner: fields[1], ID: fields[2], Raw: line}
		if len(fields) > 3 {
			job.FileName = fields[3]
		}
		if len(fields) > 4 {
			job.Size = strings.Join(fields[4:], " ")
		}
		snapshot.Jobs = append(snapshot.Jobs, job)
	}
	snapshot.Complete = headerSeen && onlyRecognizedRows
	return snapshot
}

func (s *SSH) Queue(ctx context.Context, queue string) (QueueSnapshot, error) {
	if _, ok := catalog.FindQueue(s.printers, queue); !ok {
		return QueueSnapshot{}, fmt.Errorf("unknown printer queue %q", queue)
	}
	client, parent, route, err := s.connected(ctx)
	if err != nil {
		return QueueSnapshot{}, err
	}
	defer closeClients(client, parent)
	stopCancel := context.AfterFunc(ctx, func() { closeClients(client, parent) })
	defer stopCancel()
	output, err := runRemote(ctx, client, "lpq -P "+shellQuote(queue))
	if err != nil {
		// lpq's nonzero status can still carry a useful server queue message.
		if strings.TrimSpace(output) == "" {
			return QueueSnapshot{}, fmt.Errorf("queue query failed: %w", err)
		}
	}
	snapshot := ParseQueueOutput(queue, output, time.Now().UTC())
	snapshot.Route = route
	return snapshot, nil
}

func (s *SSH) Cancel(ctx context.Context, queue, spoolerID, operationID string) error {
	printer, ok := catalog.FindQueue(s.printers, queue)
	if !ok || printer.Access != "public" {
		return errors.New("cannot cancel a job on this queue")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`).MatchString(spoolerID) {
		return errors.New("invalid print job identifier")
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(operationID) {
		return errors.New("invalid print operation identifier")
	}
	client, parent, _, err := s.connected(ctx)
	if err != nil {
		return err
	}
	defer closeClients(client, parent)
	stopCancel := context.AfterFunc(ctx, func() { closeClients(client, parent) })
	defer stopCancel()
	// Re-read the queue and only cancel a matching ID owned by this account.
	output, err := runRemote(ctx, client, "lpq -P "+shellQuote(queue))
	if err != nil {
		return fmt.Errorf("could not refresh the queue before cancelling: %w", err)
	}
	if !ownsRecordedJob(ParseQueueOutput(queue, output, time.Now()), s.username, spoolerID, operationID) {
		return errors.New("this job is no longer in the queue or is not owned by your account")
	}
	_, err = runRemote(ctx, client, "lprm -P "+shellQuote(queue)+" "+shellQuote(spoolerID))
	return err
}

func ownsRecordedJob(snapshot QueueSnapshot, username, spoolerID, operationID string) bool {
	needle := "document-" + operationID + ".pdf"
	for _, job := range snapshot.Jobs {
		if job.ID == spoolerID && job.Owner == username && strings.Contains(job.Raw, needle) {
			return true
		}
	}
	return false
}

func (s *SSH) Recover(ctx context.Context, operationIDs []string) (map[string]Receipt, error) {
	client, parent, _, err := s.connected(ctx)
	if err != nil {
		return nil, err
	}
	defer closeClients(client, parent)
	stopCancel := context.AfterFunc(ctx, func() { closeClients(client, parent) })
	defer stopCancel()
	transfer, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}
	defer transfer.Close()
	home, err := transfer.Getwd()
	if err != nil {
		return nil, err
	}
	root := pathJoin(home, ".cache", "socprint")
	results := make(map[string]Receipt)
	for _, id := range operationIDs {
		if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(id) {
			continue
		}
		file, err := transfer.Open(pathJoin(root, "op-"+id, "receipt.json"))
		if err != nil {
			continue
		}
		contents, readErr := io.ReadAll(io.LimitReader(file, 4096))
		_ = file.Close()
		if readErr != nil {
			continue
		}
		var receipt Receipt
		if json.Unmarshal(contents, &receipt) == nil && receipt.ID == id {
			results[id] = receipt
		}
	}
	return results, nil
}

func (s *SSH) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.password, s.keyPass = "", ""
	return nil
}
