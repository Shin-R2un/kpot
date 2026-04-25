//go:build !linux && !darwin && !windows

package clipboard

func detect() (Clipboard, error) { return nil, ErrUnavailable }
