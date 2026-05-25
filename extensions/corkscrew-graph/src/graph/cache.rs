// Graph cache: TTL- and fingerprint-validated, LRU-bounded.
use anyhow::Result;
use lru::LruCache;
use std::num::NonZeroUsize;
use std::sync::Arc;
use std::sync::Mutex;
use std::sync::OnceLock;
use duckdb::Connection;

use super::loader::{LoadedGraph, fingerprint_path};

const DEFAULT_CACHE_TTL_SECS: u64 = 300; // 5 minutes
const DEFAULT_CACHE_MAX_ENTRIES: usize = 4;

static GRAPH_CACHE: OnceLock<Mutex<LruCache<String, Arc<LoadedGraph>>>> = OnceLock::new();
static CACHE_LOAD_LOCK: OnceLock<Mutex<()>> = OnceLock::new();

fn cache() -> &'static Mutex<LruCache<String, Arc<LoadedGraph>>> {
    GRAPH_CACHE.get_or_init(|| {
        Mutex::new(LruCache::new(
            NonZeroUsize::new(DEFAULT_CACHE_MAX_ENTRIES).expect("non-zero default"),
        ))
    })
}

fn cache_load_lock() -> &'static Mutex<()> {
    CACHE_LOAD_LOCK.get_or_init(|| Mutex::new(()))
}

/// Reads the optional DuckDB session setting and falls back to the default TTL.
fn cache_ttl_secs(conn: &Connection) -> u64 {
    conn.query_row(
        "SELECT COALESCE(TRY_CAST(getvariable('corkscrew_graph_cache_ttl') AS UBIGINT), ?)",
        [DEFAULT_CACHE_TTL_SECS],
        |row| row.get::<_, u64>(0),
    )
    .unwrap_or(DEFAULT_CACHE_TTL_SECS)
}

/// Reads the optional max-entries session setting. Returns the default when
/// the variable is unset, malformed, or zero.
fn cache_max_entries(conn: &Connection) -> NonZeroUsize {
    let raw = conn
        .query_row(
            "SELECT COALESCE(TRY_CAST(getvariable('corkscrew_graph_cache_max_entries') AS UBIGINT), ?)",
            [DEFAULT_CACHE_MAX_ENTRIES as u64],
            |row| row.get::<_, u64>(0),
        )
        .unwrap_or(DEFAULT_CACHE_MAX_ENTRIES as u64);
    NonZeroUsize::new(raw as usize)
        .unwrap_or(NonZeroUsize::new(DEFAULT_CACHE_MAX_ENTRIES).expect("non-zero default"))
}

/// Canonicalize the cache key so symlinks and relative paths map to the same
/// entry. Falls back to the raw path for non-existent files (in-memory DBs,
/// transient races during creation).
fn cache_key(db_path: &str) -> String {
    std::fs::canonicalize(db_path)
        .map(|p| p.to_string_lossy().into_owned())
        .unwrap_or_else(|_| db_path.to_string())
}

/// An entry is fresh iff (a) it hasn't aged past TTL and (b) the file's
/// (mtime, size) fingerprint matches what we captured at load time. A `None`
/// fingerprint (in-memory DB) is matched against `None`; any divergence means
/// the file was rewritten and we need to reload.
fn entry_is_fresh(loaded: &LoadedGraph, key: &str, ttl: u64) -> bool {
    if loaded.loaded_at_instant.elapsed().as_secs() >= ttl {
        return false;
    }
    fingerprint_path(key) == loaded.file_fingerprint
}

pub fn get_or_load(conn: &Connection, db_path: &str) -> Result<Arc<LoadedGraph>> {
    let ttl = cache_ttl_secs(conn);
    let max_entries = cache_max_entries(conn);
    let key = cache_key(db_path);

    {
        let mut guard = cache().lock().unwrap();
        // `LruCache::cap()` is the current bound; resize on the fly when the
        // session variable changes so users get the configured behavior without
        // restarting.
        if guard.cap() != max_entries {
            guard.resize(max_entries);
        }
        if let Some(existing) = guard.get(&key) {
            if entry_is_fresh(existing, &key, ttl) {
                return Ok(Arc::clone(existing));
            }
        }
    }

    let _load_guard = cache_load_lock().lock().unwrap();

    {
        let mut guard = cache().lock().unwrap();
        if let Some(existing) = guard.get(&key) {
            if entry_is_fresh(existing, &key, ttl) {
                return Ok(Arc::clone(existing));
            }
        }
    }

    let providers = super::schema::detect_providers(conn)?;
    let mut loaded = super::loader::load_graph(conn, &providers)?;
    loaded.file_fingerprint = fingerprint_path(&key);
    let arc = Arc::new(loaded);
    cache().lock().unwrap().put(key, Arc::clone(&arc));
    Ok(arc)
}

pub fn invalidate(db_path: &str) {
    let key = cache_key(db_path);
    cache().lock().unwrap().pop(&key);
}
