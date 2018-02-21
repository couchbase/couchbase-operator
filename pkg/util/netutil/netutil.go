// Network utility functions

package netutil

import (
	"fmt"
	"net"
	"time"
)

// Wait for a TCP port to become available
// Checks the port once a second for 'retries' times.
// Returns nil on success or the last error on failure
func WaitForHostPort(hostport string, retries int) error {
	var lastErr error
	for i := 0; i < retries; i++ {
		conn, err := net.DialTimeout("tcp", hostport, 1*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("unable to contact %s after %d attempts (%s)", hostport, retries, lastErr.Error())
}
