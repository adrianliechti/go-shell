//go:build !darwin && !windows

package shell

import "errors"

func run(Options) error {
	return errors.ErrUnsupported
}
