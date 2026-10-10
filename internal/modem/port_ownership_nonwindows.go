//go:build !windows

package modem

func acquireATPortOwnership(string) (func(), error) { return func() {}, nil }
