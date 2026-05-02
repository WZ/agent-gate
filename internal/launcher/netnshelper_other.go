//go:build !linux

package launcher

import "errors"

func RunNetnsHelper(args []string) error {
	return errors.New("__netns-helper is linux-only")
}
