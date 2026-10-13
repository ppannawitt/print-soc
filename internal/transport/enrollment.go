package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const keyServiceHost = "skeys.comp.nus.edu.sg"

var fingerprintPattern = regexp.MustCompile(`SHA256:[A-Za-z0-9+/]+={0,2}`)

var ErrJumpKeyRejected = errors.New("the selected SSH key was rejected by the SoC jump host")
var ErrJumpEnrollmentRejected = errors.New("the SoC SSH Key Service rejected the jump-key update")
var ErrJumpEnrollmentUncertain = errors.New("the SoC SSH Key Service could not confirm the jump-key update")
var errJumpPasswordAuthentication = fmt.Errorf("%w: the jump host accepted the key but did not accept the account password", ErrAuthentication)
var errJumpAdditionalAuthentication = errors.New("the jump host accepted the key but requires an unsupported additional authentication method")

// SelectedKeyFingerprint returns the selected private key's public fingerprint.
func SelectedKeyFingerprint(path, passphrase string) (string, error) {
	signer, err := LoadPrivateKey(path, passphrase)
	if err != nil {
		if isKeyPassphraseError(err) {
			return "", fmt.Errorf("%w: %v", ErrKeyPassphrase, err)
		}
		return "", fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return ssh.FingerprintSHA256(signer.PublicKey()), nil
}

// CheckJumpKey requires the selected key to authenticate to sjump. Some SoC
// routes require publickey,password: supply the password only after the server
// confirms public-key partial success. Password fallback cannot pass this check.
// Then verify that the same jump session can reach and authenticate to stu.
func (s *SSH) CheckJumpKey(ctx context.Context) error {
	if s.keyPath == "" {
		return ErrNoKey
	}
	signer, err := LoadPrivateKey(s.keyPath, s.keyPass)
	if err != nil {
		if isKeyPassphraseError(err) {
			return fmt.Errorf("%w: %v", ErrKeyPassphrase, err)
		}
		return fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	jumpConfig := jumpKeyClientConfig(s.username, s.password, signer, s.knownHosts)
	jump, err := s.dialJumpKey(ctx, jumpConfig)
	if err != nil {
		return err
	}
	defer jump.Close()
	target, err := jump.DialContext(ctx, "tcp", hostAddress(s.unixHost))
	if err != nil {
		return fmt.Errorf("%w through the jump host: %v", ErrNetwork, err)
	}
	targetConfig := &ssh.ClientConfig{
		User: s.username, Auth: []ssh.AuthMethod{ssh.Password(s.password)},
		HostKeyCallback: s.knownHosts, Timeout: 8 * time.Second,
	}
	client, err := s.completeHandshake(target, hostAddress(s.unixHost), targetConfig)
	if err != nil {
		if errors.Is(err, ErrAuthentication) {
			return ErrAuthentication
		}
		return err
	}
	_ = client.Close()
	return nil
}

func jumpKeyClientConfig(username, password string, signer ssh.Signer, verifyHost ssh.HostKeyCallback) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: verifyHost, Timeout: 8 * time.Second,
		AuthCallback: func(auth *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			if !slices.Contains(auth.PartialSuccessMethods, "publickey") {
				return nil, nil // Only the selected public key may be attempted first.
			}
			if password == "" || slices.Contains(auth.TriedMethods, "password") {
				return nil, errJumpPasswordAuthentication
			}
			if !slices.Contains(auth.AllowedMethods, "password") {
				return nil, errJumpAdditionalAuthentication
			}
			return ssh.Password(password), nil
		},
	}
}

// dialJumpKey checks every address returned for sjump. Since the hostname can
// resolve to multiple jump servers, one backend's rejection must not stand in
// for the entire jump service.
func (s *SSH) dialJumpKey(ctx context.Context, clientConfig *ssh.ClientConfig) (*ssh.Client, error) {
	logicalAddress := hostAddress(s.jumpHost)
	host, port, err := net.SplitHostPort(logicalAddress)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid jump-host address", ErrNetwork)
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: could not resolve the jump host: %v", ErrNetwork, err)
	}
	addresses := make([]string, 0, len(resolved))
	seen := make(map[string]struct{}, len(resolved))
	for _, candidate := range resolved {
		address := net.JoinHostPort(candidate.IP.String(), port)
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: the jump host has no addresses", ErrNetwork)
	}
	return dialJumpKeyAddresses(ctx, logicalAddress, addresses, clientConfig)
}

func dialJumpKeyAddresses(ctx context.Context, logicalAddress string, addresses []string, clientConfig *ssh.ClientConfig) (*ssh.Client, error) {
	rejected := 0
	for _, address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
		}
		connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
		if err != nil {
			continue
		}
		stopCancel := context.AfterFunc(ctx, func() { _ = connection.Close() })
		client, handshakeErr := (&SSH{}).completeHandshake(connection, logicalAddress, clientConfig)
		stopCancel()
		if handshakeErr == nil {
			return client, nil
		}
		if errors.Is(handshakeErr, errJumpPasswordAuthentication) || errors.Is(handshakeErr, errJumpAdditionalAuthentication) {
			return nil, handshakeErr // The key was accepted; never report rejection.
		}
		if errors.Is(handshakeErr, ErrAuthentication) {
			rejected++
			continue
		}
		// Host-identity failures and explicit user verification requests must
		// never be hidden by another address in the DNS response.
		if errors.Is(handshakeErr, ErrChangedHostKey) {
			return nil, handshakeErr
		}
		var trust *TrustRequired
		if errors.As(handshakeErr, &trust) {
			return nil, trust
		}
	}
	if len(addresses) > 0 && rejected == len(addresses) {
		return nil, ErrJumpKeyRejected
	}
	return nil, fmt.Errorf("%w: no jump server accepted a connection", ErrNetwork)
}

// ShowJumpKeys reads the SSH Key Service's jump-host fingerprints.
func (s *SSH) ShowJumpKeys(ctx context.Context) (JumpKeyConfig, error) {
	client, err := s.keyServiceClient(ctx)
	if err != nil {
		return JumpKeyConfig{}, err
	}
	defer client.Close()
	output, err := runSSHCommand(ctx, client, "show", "")
	if err != nil {
		return JumpKeyConfig{}, fmt.Errorf("could not read the SoC SSH Key Service configuration: %w", err)
	}
	return ParseJumpKeyConfig(output)
}

// EnrollJumpKeys updates jump access only after re-reading the registered list
// and confirming every existing fingerprint is present in the merged payload.
func (s *SSH) EnrollJumpKeys(ctx context.Context, expectedCurrent []string, authorizedKeys []string) (JumpKeyConfig, error) {
	current, err := s.ShowJumpKeys(ctx)
	if err != nil {
		return JumpKeyConfig{}, err
	}
	if !sameFingerprints(current.Fingerprints, expectedCurrent) {
		return JumpKeyConfig{}, ErrJumpKeysChanged
	}
	merged, fingerprints, err := mergeAuthorizedKeys(authorizedKeys)
	if err != nil {
		return JumpKeyConfig{}, err
	}
	for _, fingerprint := range current.Fingerprints {
		if _, ok := fingerprints[fingerprint]; !ok {
			return JumpKeyConfig{}, fmt.Errorf("%w: missing %s", ErrJumpKeysUnreadable, fingerprint)
		}
	}
	if len(merged) == 0 {
		return JumpKeyConfig{}, errors.New("include at least one SSH public key")
	}
	payload := strings.Join(merged, "\n") + "\n"
	client, err := s.keyServiceClient(ctx)
	if err != nil {
		return JumpKeyConfig{}, err
	}
	output, commandErr := runSSHCommand(ctx, client, "jump", payload)
	_ = client.Close()
	// Always inspect the resulting list. The command can save successfully and
	// then lose its SSH response, so an interrupted command must never trigger a
	// blind resubmission.
	updated, err := s.ShowJumpKeys(ctx)
	if err != nil {
		if commandErr != nil {
			return JumpKeyConfig{}, fmt.Errorf("%w: verify jump keys with show before retrying: %w", ErrJumpEnrollmentUncertain, errors.Join(commandErr, err))
		}
		return JumpKeyConfig{}, fmt.Errorf("%w: %w", ErrJumpEnrollmentUncertain, err)
	}
	if err := verifyJumpEnrollment(output, commandErr, updated, fingerprints); err != nil {
		return updated, err
	}
	return updated, nil
}

func verifyJumpEnrollment(output string, commandErr error, updated JumpKeyConfig, required map[string]struct{}) error {
	if keyServiceRejected(output) {
		return fmt.Errorf("%w: %s", ErrJumpEnrollmentRejected, serviceError(output))
	}
	actual := make(map[string]struct{}, len(updated.Fingerprints))
	for _, fingerprint := range updated.Fingerprints {
		actual[fingerprint] = struct{}{}
	}
	for fingerprint := range required {
		if _, ok := actual[fingerprint]; !ok {
			if commandErr != nil {
				return fmt.Errorf("%w: key %s was absent after an interrupted update: %w", ErrJumpEnrollmentUncertain, fingerprint, commandErr)
			}
			return fmt.Errorf("%w: key %s is absent from the resulting list", ErrJumpEnrollmentUncertain, fingerprint)
		}
	}
	if !strings.Contains(output, "Authorised keys updated for jump access") {
		if commandErr != nil {
			return fmt.Errorf("%w: the resulting list contains the requested fingerprints, but the service response was interrupted: %w", ErrJumpEnrollmentUncertain, commandErr)
		}
		return fmt.Errorf("%w: the service response did not confirm an update", ErrJumpEnrollmentUncertain)
	}
	return nil
}

func keyServiceRejected(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "error:") {
			return true
		}
	}
	return false
}

func serviceError(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "error:") {
			return line
		}
	}
	return "the submitted public keys were not accepted"
}

// ParseJumpKeyConfig extracts fingerprints only from the jump-host section.
// It fails closed when the output changes or omits that section.
func ParseJumpKeyConfig(output string) (JumpKeyConfig, error) {
	var config JumpKeyConfig
	config.Output = output
	knownSection := false
	active := false
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "no ssh keys configured for jump host") {
			knownSection = true
			active = false
			continue
		}
		if strings.Contains(lower, "ssh keys configured for jump host") {
			knownSection = true
			active = true
			continue
		}
		if strings.Contains(line, "***") && active {
			active = false
		}
		if !active {
			continue
		}
		for _, fingerprint := range fingerprintPattern.FindAllString(line, -1) {
			if _, ok := seen[fingerprint]; !ok {
				seen[fingerprint] = struct{}{}
				config.Fingerprints = append(config.Fingerprints, fingerprint)
			}
		}
	}
	if !knownSection {
		return JumpKeyConfig{}, ErrJumpKeysUnreadable
	}
	sort.Strings(config.Fingerprints)
	return config, nil
}

func (s *SSH) keyServiceClient(ctx context.Context) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: s.username, Auth: []ssh.AuthMethod{ssh.Password(s.password)},
		HostKeyCallback: s.knownHosts, Timeout: 8 * time.Second,
	}
	return s.dialHost(ctx, keyServiceHost, config)
}

func (s *SSH) dialHost(ctx context.Context, host string, config *ssh.ClientConfig) (*ssh.Client, error) {
	address := hostAddress(host)
	connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancel()
	return s.completeHandshake(connection, address, config)
}

func runSSHCommand(ctx context.Context, client *ssh.Client, command, input string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return "", err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return "", err
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stopCancel()
	if err := session.Start(command); err != nil {
		return "", err
	}
	var out, diagnostic bytes.Buffer
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); _, _ = io.Copy(&out, io.LimitReader(stdout, 1<<20)) }()
	go func() { defer readers.Done(); _, _ = io.Copy(&diagnostic, io.LimitReader(stderr, 1<<20)) }()
	if input != "" {
		if _, err := io.WriteString(stdin, input); err != nil {
			_ = stdin.Close()
			return out.String(), err
		}
	}
	if err := stdin.Close(); err != nil {
		return out.String(), err
	}
	waitErr := session.Wait()
	readers.Wait()
	combined := out.String()
	if diagnostic.Len() > 0 {
		combined += "\n" + diagnostic.String()
	}
	return combined, waitErr
}

func mergeAuthorizedKeys(lines []string) ([]string, map[string]struct{}, error) {
	merged := make([]string, 0, len(lines))
	fingerprints := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		publicKey, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, nil, errors.New("one or more SSH public-key lines are invalid")
		}
		if len(bytes.TrimSpace(rest)) != 0 {
			return nil, nil, errors.New("enter one SSH public key per line")
		}
		fingerprint := ssh.FingerprintSHA256(publicKey)
		if _, exists := fingerprints[fingerprint]; exists {
			continue
		}
		fingerprints[fingerprint] = struct{}{}
		merged = append(merged, line)
	}
	return merged, fingerprints, nil
}

// MergeAuthorizedKeys validates and deduplicates complete authorized-key lines.
func MergeAuthorizedKeys(lines []string) ([]string, map[string]struct{}, error) {
	return mergeAuthorizedKeys(lines)
}

func sameFingerprints(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	set := make(map[string]struct{}, len(first))
	for _, fingerprint := range first {
		set[fingerprint] = struct{}{}
	}
	for _, fingerprint := range second {
		if _, ok := set[fingerprint]; !ok {
			return false
		}
	}
	return true
}
