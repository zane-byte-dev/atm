package store

import "database/sql"

// Derived dependency fingerprints live in sync_state alongside file versions.
// The reserved namespace/agent cannot collide with a transcript, is not touched
// by per-agent retention, and is cleared with derived mirrors in compact backups.
const syncCacheAgent = "_sync_cache"
const syncCachePrefix = "atm-cache://v1/"

type cacheReader interface{ QueryRow(string, ...any) *sql.Row }
type cacheWriter interface {
	Exec(string, ...any) (sql.Result, error)
}

func syncCacheMatches(db cacheReader, key string, first, second, version int64) bool {
	var a, b, v int64
	err := db.QueryRow("SELECT mtime_unix,size_bytes,offset_bytes FROM sync_state WHERE file_path=? AND agent=?", syncCachePrefix+key, syncCacheAgent).Scan(&a, &b, &v)
	return err == nil && a == first && b == second && v == version
}

func saveSyncCache(db cacheWriter, key string, first, second, version int64) error {
	_, err := db.Exec(`INSERT INTO sync_state(file_path,agent,mtime_unix,size_bytes,offset_bytes) VALUES (?,?,?,?,?)
		ON CONFLICT(file_path) DO UPDATE SET mtime_unix=excluded.mtime_unix,size_bytes=excluded.size_bytes,offset_bytes=excluded.offset_bytes`, syncCachePrefix+key, syncCacheAgent, first, second, version)
	return err
}
