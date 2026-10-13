package credentials

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

const service = "NUS School of Computing SoC Print"

func passwordKey(username string) string   { return "password:" + username }
func passphraseKey(username string) string { return "key-passphrase:" + username }
func fingerprintPassphraseKey(username, fingerprint string) string {
	fingerprint = strings.NewReplacer(":", "_", "/", "_", "+", "_").Replace(fingerprint)
	return "key-passphrase:" + username + ":" + fingerprint
}

func GetPassword(username string) (string, error) { return keyring.Get(service, passwordKey(username)) }
func SetPassword(username, password string) error {
	return keyring.Set(service, passwordKey(username), password)
}
func DeletePassword(username string) error {
	err := keyring.Delete(service, passwordKey(username))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
func GetPassphrase(username string) (string, error) {
	return keyring.Get(service, passphraseKey(username))
}
func SetPassphrase(username, value string) error {
	return keyring.Set(service, passphraseKey(username), value)
}
func DeletePassphrase(username string) error {
	err := keyring.Delete(service, passphraseKey(username))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func GetPassphraseForKey(username, fingerprint string) (string, error) {
	if fingerprint != "" {
		value, err := keyring.Get(service, fingerprintPassphraseKey(username, fingerprint))
		if err == nil {
			return value, nil
		}
		if !errors.Is(err, keyring.ErrNotFound) {
			return "", err
		}
	}
	return GetPassphrase(username)
}

func SetPassphraseForKey(username, fingerprint, value string) error {
	if username == "" || fingerprint == "" {
		return errors.New("account and key fingerprint are required")
	}
	return keyring.Set(service, fingerprintPassphraseKey(username, fingerprint), value)
}

func DeletePassphraseForKey(username, fingerprint string) error {
	if fingerprint == "" {
		return nil
	}
	err := keyring.Delete(service, fingerprintPassphraseKey(username, fingerprint))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
