//go:build !darwin

package main

import "net"

// Binding LocalAddr to the selected interface address is the portable path.
// Windows does not expose Darwin's IP_BOUND_IF socket option.
func bindDialerToInterface(_ *net.Dialer, _ int) {}
