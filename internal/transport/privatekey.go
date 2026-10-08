package transport

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/ssh"
)

// LoadPrivateKey returns the SSH signer produced by parsing a private key.
// ParsePrivateKey already constructs the signer; converting it again rejects
// valid keys. A saved passphrase is ignored when the key is unencrypted.
func LoadPrivateKey(path, passphrase string) (ssh.Signer, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("choose a regular SSH private key no larger than 1 MB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > 1<<20 {
		return nil, fmt.Errorf("the SSH private key exceeds 1 MB")
	}
	signer, err := ssh.ParsePrivateKey(contents)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) && passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(contents, []byte(passphrase))
	}
	return signer, err
}
