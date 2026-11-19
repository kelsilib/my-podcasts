package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const encryptedCredentialsFile = "auth.dpapi"

func readStoredCredentials(dir string) ([]byte, error) {
	if dir == "" {
		return nil, os.ErrNotExist
	}
	raw, err := os.ReadFile(filepath.Join(dir, encryptedCredentialsFile))
	if err != nil {
		return nil, err
	}
	input := windows.DataBlob{Size: uint32(len(raw)), Data: unsafe.SliceData(raw)}
	var plain windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &plain); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(plain.Data))))
	return bytes.Clone(unsafe.Slice(plain.Data, plain.Size)), nil
}

func writeStoredCredentials(dir string, raw []byte) error {
	if dir == "" {
		return errors.New("user data directory unavailable")
	}
	input := windows.DataBlob{Size: uint32(len(raw)), Data: unsafe.SliceData(raw)}
	var encrypted windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &encrypted); err != nil {
		return err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(encrypted.Data))))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dir, encryptedCredentialsFile), unsafe.Slice(encrypted.Data, encrypted.Size), 0o600)
}

func removeStoredCredentials(dir string) error {
	if dir == "" {
		return nil
	}
	return removeIfPresent(filepath.Join(dir, encryptedCredentialsFile))
}
