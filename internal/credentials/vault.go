package credentials

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const service = "NUS School of Computing SoC Print"

func passwordKey(username string) string   { return "password:" + username }
func passphraseKey(username string) string { return "key-passphrase:" + username }

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
