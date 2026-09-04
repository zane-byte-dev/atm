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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	workspaceContentVerifyEvery = time.Minute
)

// ChangeTracker keeps one connection for PRAGMA data_version. Comparing values
// from different pooled connections would miss other-process commits.
type ChangeTracker struct {
	mu    sync.Mutex
	dir   string
	db    *sql.DB
	conn  *sql.Conn
	files map[string]*workspaceHashState
}

type workspaceHashFile struct {
	info   fs.FileInfo
	digest [sha256.Size]byte
}

type workspaceHashState struct {
	files        map[string]workspaceHashFile
	signature    string
	lastVerified time.Time
	contentReads uint64
}

func NewChangeTracker(dataDir string) *ChangeTracker {
	return &ChangeTracker{dir: dataDir, files: map[string]*workspaceHashState{}}
}
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
	tracker.files = nil
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
			signature, err := tracker.hashWorkspaceFiles(ctx, filepath.Join(tracker.dir, path))
			if err != nil {
				return nil, err
			}
			value += ":" + signature
		}
		result[domain] = value
	}
	return result, nil
}

func (tracker *ChangeTracker) hashWorkspaceFiles(ctx context.Context, root string) (string, error) {
	now := time.Now()
	state := tracker.files[root]
	if state == nil {
		state = &workspaceHashState{files: map[string]workspaceHashFile{}}
		tracker.files[root] = state
	}
	verifyContent := state.lastVerified.IsZero() || now.Sub(state.lastVerified) >= workspaceContentVerifyEvery
	signature, err := hashWorkspaceFilesIncremental(ctx, root, state, verifyContent)
	if err != nil {
		return "", err
	}
	state.signature = signature
	if verifyContent {
		state.lastVerified = now
	}
	return signature, nil
}

// Only known workspace roots are examined. Symlinks are represented, never
// followed. Hashing content also catches same-length writes and atomic replace.
func hashWorkspaceFiles(ctx context.Context, root string) (string, error) {
	return hashWorkspaceFilesIncremental(ctx, root, &workspaceHashState{files: map[string]workspaceHashFile{}}, true)
}

func hashWorkspaceFilesIncremental(ctx context.Context, root string, state *workspaceHashState, verifyContent bool) (string, error) {
	hash := sha256.New()
	type workspacePath struct {
		path string
		info fs.FileInfo
	}
	var paths []workspacePath
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
			paths = append(paths, workspacePath{path: path})
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		paths = append(paths, workspacePath{path: path, info: info})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].path < paths[j].path })
	base := filepath.Dir(root)
	anchor, err := os.OpenRoot(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && len(paths) == 0 {
			return fmt.Sprintf("%x", hash.Sum(nil)), nil
		}
		return "", err
	}
	defer anchor.Close()
	buffer := make([]byte, 32*1024)
	next := make(map[string]workspaceHashFile, len(paths))
	for _, item := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if item.info == nil {
			digest := sha256.Sum256([]byte("symlink:" + item.path))
			next[item.path] = workspaceHashFile{digest: digest}
			fmt.Fprintf(hash, "%s\x00%x\x00", item.path, digest)
			continue
		}
		cached, reusable := state.files[item.path]
		reusable = reusable && !verifyContent && sameWorkspaceFileInfo(cached.info, item.info)
		if reusable {
			next[item.path] = cached
			fmt.Fprintf(hash, "%s\x00%x\x00", item.path, cached.digest)
			continue
		}
		// Root.Open prevents a replaced parent directory from escaping the
		// data root between traversal and opening the file.
		relative, err := filepath.Rel(base, item.path)
		if err != nil {
			return "", err
		}
		file, err := anchor.Open(relative)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		contentHash := sha256.New()
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			var count int
			count, err = file.Read(buffer)
			if count > 0 {
				contentHash.Write(buffer[:count])
			}
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				break
			}
		}
		info, statErr := file.Stat()
		file.Close()
		if err != nil {
			return "", err
		}
		if statErr != nil {
			return "", statErr
		}
		var digest [sha256.Size]byte
		copy(digest[:], contentHash.Sum(nil))
		state.contentReads++
		next[item.path] = workspaceHashFile{info: info, digest: digest}
		fmt.Fprintf(hash, "%s\x00%x\x00", item.path, digest)
	}
	state.files = next
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func sameWorkspaceFileInfo(left, right fs.FileInfo) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Size() == right.Size() &&
		left.Mode() == right.Mode() &&
		left.ModTime().Equal(right.ModTime()) &&
		os.SameFile(left, right) &&
		reflect.DeepEqual(left.Sys(), right.Sys())
}
