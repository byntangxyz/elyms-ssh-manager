//go:build !windows

package keygen

import "os"

func restrictPrivate(path string) error {
	return os.Chmod(path, 0o600)
}
