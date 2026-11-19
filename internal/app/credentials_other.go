//go:build !windows

package app

import (
	"errors"
	"os"

	"github.com/egoist/mygo"
)

const authSessionSecret = "auth-session"

func readStoredCredentials(string) ([]byte, error) {
	raw, err := mygo.Secrets.Get(authSessionSecret)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, os.ErrNotExist
	}
	return raw, err
}

func writeStoredCredentials(_ string, raw []byte) error {
	return mygo.Secrets.Set(authSessionSecret, raw)
}

func removeStoredCredentials(string) error {
	return mygo.Secrets.Delete(authSessionSecret)
}
