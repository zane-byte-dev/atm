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
	"sync/atomic"
	"time"
)

const (
	workspaceContentVerifyEvery = time.Minute
)

var errChangeTrackerClosed = errors.New("workspace change tracker is closed")

type ChangeTracker struct {
	mu             sync.Mutex
	closed         atomic.Bool
	dir            string
	db             *sql.DB
	files          map[string]*workspaceHashState
	projectTodos   func(context.Context, string, map[string]int64) error
	projectedTodos map[string]int64
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

// SetTodoDocumentProjector enables the resident watcher's derived timestamp
// projection. Ordinary trackers remain read-only, and HTTP reads never call it.
func (tracker *ChangeTracker) SetTodoDocumentProjector(project func(context.Context, string, map[string]int64) error) {
	if tracker.closed.Load() {
		return
	}
	tracker.mu.Lock()
	defer tracker.unlock()
	if tracker.closed.Load() {
		return
	}
	tracker.projectTodos = project
	tracker.projectedTodos = make(map[string]int64)
}
func (tracker *ChangeTracker) Close() {
	tracker.closed.Store(true)
	// Server shutdown drains the watcher first. If an uncooperative callback
	// exceeds that budget, do not block process exit here or tear down resources
	// it is still using. Its final unlock will perform the deferred cleanup.
	if tracker.mu.TryLock() {
		tracker.closeLocked()
		tracker.mu.Unlock()
	}
}

func (tracker *ChangeTracker) unlock() {
	tracker.mu.Unlock()
	// Check after unlocking so Close cannot miss cleanup by racing between a
	// closed check and the unlock. Close retries safely under the same mutex.
	if tracker.closed.Load() {
		tracker.Close()
	}
}

func (tracker *ChangeTracker) closeLocked() {
	if tracker.db != nil {
		_ = tracker.db.Close()
		tracker.db = nil
	}
	tracker.files = nil
	tracker.projectTodos = nil
	tracker.projectedTodos = nil
}

func (tracker *ChangeTracker) Fingerprints(ctx context.Context, domains []string) (map[string]string, error) {
	if tracker.closed.Load() {
		return nil, errChangeTrackerClosed
	}
	tracker.mu.Lock()
	defer tracker.unlock()
	if tracker.closed.Load() {
		return nil, errChangeTrackerClosed
	}
	result := map[string]string{}
	if len(domains) == 0 {
		return result, ctx.Err()
	}
	if tracker.db == nil {
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
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return nil, err
		}
		tracker.db = db
	}
	// Read domain revisions together from one SQLite statement/snapshot.
	args := make([]any, len(domains))
	for index, domain := range domains {
		args[index] = domain
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := tracker.db.QueryContext(ctx, `SELECT domain,revision FROM workspace_changes WHERE domain IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	revisions := make(map[string]int64, len(domains))
	for rows.Next() {
		var domain string
		var revision int64
		if err := rows.Scan(&domain, &revision); err != nil {
			rows.Close()
			return nil, err
		}
		revisions[domain] = revision
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, domain := range domains {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value := strconv.FormatInt(revisions[domain], 10)
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
		if domain == "todos" && tracker.projectTodos != nil {
			changed, err := tracker.projectTodoDocumentTimes(ctx)
			if err != nil {
				return nil, err
			}
			if changed {
				// Projection is durable before observers refetch rows ordered by
				// updated_at. Rebaseline its revision in this same notification.
				var revision int64
				if err := tracker.db.QueryRowContext(ctx, `SELECT revision FROM workspace_changes WHERE domain='todos'`).Scan(&revision); err != nil {
					return nil, err
				}
				value = strconv.FormatInt(revision, 10) + strings.TrimPrefix(value, strconv.FormatInt(revisions[domain], 10))
			}
		}
		result[domain] = value
	}
	return result, nil
}

func (tracker *ChangeTracker) projectTodoDocumentTimes(ctx context.Context) (bool, error) {
	root := filepath.Join(tracker.dir, "todos")
	state := tracker.files[root]
	if state == nil {
		return false, nil
	}
	observed := make(map[string]int64)
	for path, file := range state.files {
		if file.info == nil || filepath.Dir(path) != root || filepath.Ext(path) != ".md" {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(path), ".md")
		if len(id) < 2 || id[0] != 't' {
			continue
		}
		number, err := strconv.ParseUint(id[1:], 10, 64)
		if err != nil || number == 0 || "t"+strconv.FormatUint(number, 10) != id {
			continue
		}
		observed[id] = max(0, file.info.ModTime().Unix())
	}
	var pending []string
	for id, stamp := range observed {
		if previous, ok := tracker.projectedTodos[id]; !ok || previous != stamp {
			pending = append(pending, id)
		}
	}
	sort.Strings(pending)
	changed := len(pending) > 0
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		count := min(len(pending), 200)
		batch := make(map[string]int64, count)
		for _, id := range pending[:count] {
			batch[id] = observed[id]
		}
		if err := tracker.projectTodos(ctx, tracker.dir, batch); err != nil {
			return false, err
		}
		for id, stamp := range batch {
			tracker.projectedTodos[id] = stamp
		}
		pending = pending[count:]
	}
	for id := range tracker.projectedTodos {
		if _, exists := observed[id]; !exists {
			delete(tracker.projectedTodos, id)
		}
	}
	return changed, nil
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
