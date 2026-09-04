package presence

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
)

type hookState struct {
	event         agentevent.Envelope
	eventAt       time.Time
	receivedAt    time.Time
	expiresAt     time.Time
	state         string
	turnReporting bool
}

type notificationEntry struct {
	notification Notification
	delivered    bool
	offered      bool
}

type Runtime struct {
	mu             sync.Mutex
	opts           Options
	owner          Owner
	listener       *net.UnixListener
	socketInfo     os.FileInfo
	locks          []*os.File
	connections    map[net.Conn]struct{}
	base           []Session
	hooks          map[string]hookState
	primed         bool
	lastEventAt    *time.Time
	sequence       uint64
	seen           map[string]uint64
	entries        []notificationEntry
	companionID    string
	companionUntil time.Time
	// systemNotificationsDisabled is the Menu user's durable display choice.
	// Notification entries remain in the read-only feed while this is true;
	// only delivery through the OS fallback is suppressed.
	systemNotificationsDisabled bool
	// Banners at or below this sequence were already queued when the user
	// disabled notifications. Keep rejecting them after a rapid re-enable.
	suppressedFallbackThrough uint64
	closed                      bool
	stop                        chan struct{}
	changed                     chan struct{}
	deliver                     chan Notification
	wg                          sync.WaitGroup
	closeOnce                   sync.Once
	closeErr                    error
}

// Start refuses a live socket, takes OS locks for both the data directory and
// socket path, then publishes the permanent Go ownership choice. It never starts
// an old App, scans transcripts, migrates a database, or sends startup banners.
func Start(opts Options) (_ *Runtime, err error) {
	if strings.TrimSpace(opts.DataDir) == "" {
		return nil, errors.New("presence requires a data directory")
	}
	opts.DataDir, err = filepath.Abs(opts.DataDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(opts.DataDir, 0700); err != nil {
		return nil, err
	}
	opts.DataDir, err = filepath.EvalSymlinks(opts.DataDir)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(opts.DataDir, "runtime")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("presence runtime directory must be a real directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.InstanceID == "" {
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return nil, err
		}
		opts.InstanceID = hex.EncodeToString(id[:])
	}
	if opts.SocketPath == "" {
		opts.SocketPath = os.Getenv(agentevent.SocketEnvVar)
	}
	if opts.SocketPath == "" {
		opts.SocketPath = filepath.Join(opts.DataDir, "notch.sock")
	}
	if !filepath.IsAbs(opts.SocketPath) {
		return nil, errors.New("Agent hook socket path must be absolute")
	}
	if err = agentevent.CheckSocketPath(opts.SocketPath); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(opts.SocketPath), 0700); err != nil {
		return nil, err
	}

	r := &Runtime{opts: opts, connections: make(map[net.Conn]struct{}), hooks: make(map[string]hookState), seen: make(map[string]uint64), stop: make(chan struct{}), changed: make(chan struct{}, 1), deliver: make(chan Notification, MaxNotifications)}
	defer func() {
		if err == nil {
			return
		}
		if r.listener != nil {
			_ = r.listener.Close()
			r.removeOwnSocket()
		}
		for _, lock := range r.locks {
			releaseLock(lock)
		}
	}()
	for _, path := range []string{filepath.Join(dir, "presence.lock"), opts.SocketPath + ".lock"} {
		lock, lockErr := acquireLock(path)
		if lockErr != nil {
			return nil, lockErr
		}
		r.locks = append(r.locks, lock)
	}
	if err = prepareSocket(opts.SocketPath); err != nil {
		return nil, err
	}
	r.listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: opts.SocketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen for Agent hooks: %w", err)
	}
	// net.UnixListener otherwise unlinks blindly on Close, even if a different
	// listener replaced the path after this instance started.
	r.listener.SetUnlinkOnClose(false)
	r.socketInfo, err = os.Lstat(opts.SocketPath)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(opts.SocketPath, 0600); err != nil {
		return nil, err
	}
	if err = r.loadCursor(); err != nil {
		return nil, err
	}
	now := opts.Now().UTC()
	r.owner = Owner{Version: 1, Owner: "go", InstanceID: opts.InstanceID, PID: os.Getpid(), StartedAt: now, ExpiresAt: now.Add(OwnerLease), Running: true}
	if err = r.writeOwner(); err != nil {
		return nil, err
	}
	r.wg.Add(2)
	go r.accept()
	go r.maintain()
	return r, nil
}

func prepareSocket(path string) error {
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if before.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%w: path is not a socket", ErrSocketOwned)
	}
	conn, dialErr := net.DialTimeout("unix", path, 120*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return ErrSocketOwned
	}
	// A timeout or permission failure is not evidence that the owner is gone.
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("%w: cannot verify stale socket", ErrSocketOwned)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		return ErrSocketOwned
	}
	return os.Remove(path)
}

func (r *Runtime) removeOwnSocket() {
	current, err := os.Lstat(r.opts.SocketPath)
	if err == nil && r.socketInfo != nil && os.SameFile(current, r.socketInfo) {
		_ = os.Remove(r.opts.SocketPath)
	}
}

func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		close(r.stop)
		_ = r.listener.Close()
		for conn := range r.connections {
			_ = conn.Close()
		}
		r.mu.Unlock()
		r.wg.Wait()
		r.removeOwnSocket()
		// Ownership is a user migration choice. Keep it after shutdown so an old
		// App cannot silently resume its timers/socket during a Go restart.
		r.owner.Running = false
		r.owner.ExpiresAt = r.opts.Now().UTC()
		r.closeErr = r.writeOwner()
		for _, lock := range r.locks {
			releaseLock(lock)
		}
	})
	return r.closeErr
}

func (r *Runtime) accept() {
	defer r.wg.Done()
	for {
		conn, err := r.listener.AcceptUnix()
		if err != nil {
			return
		}
		r.mu.Lock()
		if r.closed || len(r.connections) >= 64 {
			r.mu.Unlock()
			_ = conn.Close()
			continue
		}
		r.connections[conn] = struct{}{}
		r.wg.Add(1)
		r.mu.Unlock()
		go r.readConnection(conn)
	}
}

func (r *Runtime) readConnection(conn net.Conn) {
	defer r.wg.Done()
	defer func() { _ = conn.Close(); r.mu.Lock(); delete(r.connections, conn); r.mu.Unlock() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	scanner := bufio.NewScanner(io.LimitReader(conn, 256<<10))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for count := 0; count < 32 && scanner.Scan(); count++ {
		line := scanner.Bytes()
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &header) != nil {
			continue
		}
		switch header.Type {
		case "":
			var event agentevent.Envelope
			if json.Unmarshal(line, &event) == nil {
				_ = r.Apply(event)
			}
		case agentevent.TypeGuardRequest:
			var guard agentevent.GuardEnvelope
			if json.Unmarshal(line, &guard) == nil {
				_ = r.applyGuard(guard)
			}
		case notificationType:
			var envelope notificationEnvelope
			ok := json.Unmarshal(line, &envelope) == nil && envelope.Version == agentevent.Version
			if ok {
				envelope.Notification.DedupKey = envelope.DedupKey
				ok = validForwardedNotification(envelope.Notification)
			}
			if ok {
				_, err := r.Publish(envelope.Notification)
				ok = err == nil
			}
			_ = json.NewEncoder(conn).Encode(struct {
				OK bool `json:"ok"`
			}{ok})
		}
	}
}

func (r *Runtime) maintain() {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastLease := r.opts.Now()
	for {
		select {
		case <-r.stop:
			return
		case <-r.changed:
			if r.opts.OnChange != nil {
				r.opts.OnChange()
			}
		case notification := <-r.deliver:
			if r.opts.Notify != nil {
				r.opts.Notify(notification)
			}
		case <-ticker.C:
			r.expire()
			if now := r.opts.Now(); now.Sub(lastLease) >= 10*time.Second {
				r.owner.ExpiresAt = now.UTC().Add(OwnerLease)
				_ = r.writeOwner()
				lastLease = now
			}
		}
	}
}

func (r *Runtime) signalChange() {
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *Runtime) writeOwner() error {
	return writeJSON(filepath.Join(r.opts.DataDir, "runtime", OwnerFile), r.owner)
}

func writeJSON(path string, value any) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".presence-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		err = json.NewEncoder(file).Encode(value)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// ReadOwner reports the persisted choice, including a stopped or expired Go
// owner. Liveness requires Running and an unexpired ExpiresAt; ownership does not.
func ReadOwner(dataDir string) (Owner, error) {
	var owner Owner
	file, err := os.Open(filepath.Join(dataDir, "runtime", OwnerFile))
	if err != nil {
		return owner, err
	}
	defer file.Close()
	err = json.NewDecoder(io.LimitReader(file, 8192)).Decode(&owner)
	if err == nil && (owner.Version != 1 || owner.Owner != "go" || owner.InstanceID == "") {
		err = errors.New("invalid presence owner marker")
	}
	return owner, err
}
