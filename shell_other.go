//go:build !darwin && !windows

package shell

import "errors"

func run(Options) error {
	return errors.ErrUnsupported
}

func pickFolder(string) (string, error) {
	return "", errors.ErrUnsupported
}
