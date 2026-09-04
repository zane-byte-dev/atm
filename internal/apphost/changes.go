package apphost

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ChangeTracker keeps one connection for PRAGMA data_version. Comparing values
// from different pooled connections would miss other-process commits.
type ChangeTracker struct {
	mu   sync.Mutex
	dir  string
	db   *sql.DB
	conn *sql.Conn
}

func NewChangeTracker(dataDir string) *ChangeTracker { return &ChangeTracker{dir: dataDir} }
func (tracker *ChangeTracker) Close() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.conn != nil {
		_ = tracker.conn.Close()
		tracker.conn = nil
	}
	if tracker.db != nil {
		_ = tracker.db.Close()
		tracker.db = nil
	}
}

func (tracker *ChangeTracker) Fingerprints(ctx context.Context, domains []string) (map[string]string, error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	result := map[string]string{}
	if tracker.conn == nil {
		path := filepath.Join(tracker.dir, "atm.db")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			for _, domain := range domains {
				result[domain] = "missing"
			}
			return result, nil
		} else if err != nil {
			return nil, err
		}
		dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(1000)"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			db.Close()
			return nil, err
		}
		tracker.db, tracker.conn = db, conn
	}
	var version int64
	if err := tracker.conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&version); err != nil {
		return nil, err
	}
	var tracked int
	if err := tracker.conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='workspace_changes'`).Scan(&tracked); err != nil {
		return nil, err
	}
	for _, domain := range domains {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value := strconv.FormatInt(version, 10)
		if tracked != 0 {
			var revision int64
			err := tracker.conn.QueryRowContext(ctx, `SELECT revision FROM workspace_changes WHERE domain=?`, domain).Scan(&revision)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			value = strconv.FormatInt(revision, 10)
		}
		var paths []string
		switch domain {
		case "todos":
			paths = []string{"todos"}
		case "knowledge":
			paths = []string{"knowledge"}
		case "settings":
			paths = []string{"config.json"}
		case "usage":
			paths = []string{"runtime/quota.json"}
		}
		for _, path := range paths {
			signature, err := hashWorkspaceFiles(ctx, filepath.Join(tracker.dir, path))
			if err != nil {
				return nil, err
			}
			value += ":" + signature
		}
		result[domain] = value
	}
	return result, nil
}

// Only known workspace roots are examined. Symlinks are represented, never
// followed. Hashing content also catches same-length writes and atomic replace.
func hashWorkspaceFiles(ctx context.Context, root string) (string, error) {
	hash := sha256.New()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "assets") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			fmt.Fprintf(hash, "symlink:%s\n", path)
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	buffer := make([]byte, 32*1024)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// Root.Open prevents a replaced parent directory from escaping the
		// data root between traversal and opening the file.
		anchor, err := os.OpenRoot(filepath.Dir(root))
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			anchor.Close()
			return "", err
		}
		file, err := anchor.Open(relative)
		anchor.Close()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00", path)
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			var count int
			count, err = file.Read(buffer)
			if count > 0 {
				hash.Write(buffer[:count])
			}
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				break
			}
		}
		file.Close()
		if err != nil {
			return "", err
		}
		hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
