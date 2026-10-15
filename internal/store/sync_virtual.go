package store

import (
	"context"
	"database/sql"
	"os"

	"github.com/zane-byte-dev/atm/internal/parser"
)

func syncVirtualSource(ctx context.Context, db *sql.DB, agent, path string) (int, error) {
	first, second, err := parser.SQLiteSourceVersion(path)
	if os.IsNotExist(err) {
		// Preserve historical sessions while forgetting their live source state.
		if _, deleteErr := db.Exec("DELETE FROM sync_state WHERE file_path=?", syncCachePrefix+"source/"+agent); deleteErr != nil {
			return 0, deleteErr
		}
		return 0, forgetRemovedSources(db, agent, map[string]bool{})
	}
	if err != nil {
		return 0, err
	}
	if syncCacheMatches(db, "source/"+agent, first, second, int64(parser.CurrentSessionParserVersion)) {
		return 0, nil
	}
	snapshot, err := parser.OpenSyncSnapshot(ctx, agent, path)
	if err != nil {
		return 0, err
	}
	defer snapshot.Close()
	files, err := snapshot.Discover()
	if err != nil {
		return 0, err
	}
	onDisk := make(map[string]bool, len(files))
	synced := 0
	for _, fp := range files {
		if err := ctx.Err(); err != nil {
			return synced, err
		}
		onDisk[fp] = true
		a, b, err := snapshot.Version(fp)
		if err != nil {
			return synced, err
		}
		var oldA, oldB int64
		var version int
		err = db.QueryRow(`SELECT st.mtime_unix,st.size_bytes,COALESCE((SELECT MAX(parser_version) FROM sessions WHERE file_path=st.file_path),0) FROM sync_state st WHERE file_path=?`, fp).Scan(&oldA, &oldB, &version)
		if err == nil && version >= parser.CurrentSessionParserVersion && a == oldA && b == oldB {
			continue
		}
		if err != nil && err != sql.ErrNoRows {
			return synced, err
		}
		parsed, err := snapshot.Parse(fp)
		if err != nil {
			return synced, err
		}
		if err := upsertSession(db, parsed, fp, agent, a, b); err != nil {
			return synced, err
		}
		synced++
	}
	if err := ctx.Err(); err != nil {
		return synced, err
	}
	if err := forgetRemovedSources(db, agent, onDisk); err != nil {
		return synced, err
	}
	// A source written during this read must be revisited on the next run.
	endA, endB, err := parser.SQLiteSourceVersion(path)
	if err == nil && first == endA && second == endB {
		err = saveSyncCache(db, "source/"+agent, first, second, int64(parser.CurrentSessionParserVersion))
	}
	return synced, err
}
