package agentevent

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// SocketEnvVar overrides the socket location, mainly for tests.
const SocketEnvVar = "ATM_NOTCH_SOCKET"

// DeliverTimeout bounds the whole delivery. A hook runs inside the agent's turn,
// so the ceiling has to be short enough that a wedged listener is invisible to
// the user rather than a stall in their session.
const DeliverTimeout = 400 * time.Millisecond

// maxSocketPathLength is the portable ceiling on sockaddr_un.sun_path. Darwin
// allows 104 bytes including the terminator; exceeding it fails with a bare
// "invalid argument" from bind/connect, so check it here and say why.
const maxSocketPathLength = 103

// SocketPath returns the presence socket path.
//
// The socket lives under ~/.atm rather than /tmp on purpose: /tmp is
// world-writable, so any local process could pre-create the path and receive
// events (which carry prompt and reply text) meant for the presence runtime.
func SocketPath() string {
	if override := os.Getenv(SocketEnvVar); override != "" {
		return override
	}
	return filepath.Join(config.AtmDir, "notch.sock")
}

// CheckSocketPath reports why a socket path cannot be used, so both the sender
// and the installer can explain the problem instead of surfacing errno.
func CheckSocketPath(path string) error {
	if path == "" {
		return errors.New("socket path is empty")
	}
	if len(path) > maxSocketPathLength {
		return fmt.Errorf(
			"socket path is %d bytes, over the %d-byte unix socket limit: %s",
			len(path), maxSocketPathLength, path,
		)
	}
	return nil
}

// Deliver sends one envelope to the presence runtime.
//
// Every failure mode is the caller's cue to do nothing: the runtime is not
// running, the socket file is stale, or the listener is wedged. None of them are
// the agent's problem, so callers report success regardless — see the CLI
// command, which exits 0 and writes nothing to stdout no matter what happens.
func Deliver(envelope Envelope) error {
	line, err := envelope.Line()
	if err != nil {
		return err
	}
	path := SocketPath()
	if err := CheckSocketPath(path); err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", path, DeliverTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(DeliverTimeout)); err != nil {
		return err
	}
	_, err = conn.Write(line)
	return err
}
