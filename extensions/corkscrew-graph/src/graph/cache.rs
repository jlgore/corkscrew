// Graph cache: TTL- and fingerprint-validated, LRU-bounded.
use anyhow::Result;
use duckdb::Connection;
use lru::LruCache;
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use std::num::NonZeroUsize;
use std::sync::Arc;
use std::sync::OnceLock;
use std::sync::{Mutex, MutexGuard, PoisonError};

use super::loader::{fingerprint_path, LoadedGraph};

const DEFAULT_CACHE_TTL_SECS: u64 = 300; // 5 minutes
const DEFAULT_CACHE_MAX_ENTRIES: usize = 4;
/// Each entry is a whole graph (up to `NODE_COUNT_MAX` nodes), so the user setting is
/// clamped; 16 covers every database a session realistically queries side by side.
pub const CACHE_ENTRIES_MAX: usize = 16;
/// Loads of keys on different stripes run concurrently. One stripe per cache entry
/// lets a full cache's worth of databases load at once, barring hash collisions.
const LOAD_LOCK_STRIPES: usize = CACHE_ENTRIES_MAX;
const _: () = assert!(DEFAULT_CACHE_MAX_ENTRIES >= 1);
const _: () = assert!(DEFAULT_CACHE_MAX_ENTRIES <= CACHE_ENTRIES_MAX);

static GRAPH_CACHE: OnceLock<Mutex<LruCache<String, Arc<LoadedGraph>>>> = OnceLock::new();
/// Striped load locks: a fixed array, so the lock set never grows with the
/// number of distinct database paths a process has seen.
static LOAD_LOCKS: [Mutex<()>; LOAD_LOCK_STRIPES] = [const { Mutex::new(()) }; LOAD_LOCK_STRIPES];

fn cache() -> &'static Mutex<LruCache<String, Arc<LoadedGraph>>> {
    GRAPH_CACHE.get_or_init(|| {
        Mutex::new(LruCache::new(
            NonZeroUsize::new(DEFAULT_CACHE_MAX_ENTRIES).expect("non-zero default"),
        ))
    })
}

/// Locks `mutex`, recovering from poisoning. A panic in one query (now caught
/// at the vtab boundary) must not make every later query panic too. Recovery
/// is sound here: the load locks guard `()`, and every LRU mutation is a single
/// call whose worst outcome is a stale entry, which `entry_is_fresh` rejects.
fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
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

/// Reads the optional max-entries session setting, clamped to
/// `1..=CACHE_ENTRIES_MAX`. Returns the default when the variable is unset or
/// malformed.
pub fn cache_max_entries(conn: &Connection) -> NonZeroUsize {
    let raw = conn
        .query_row(
            "SELECT COALESCE(TRY_CAST(getvariable('corkscrew_graph_cache_max_entries') \
             AS UBIGINT), ?)",
            [DEFAULT_CACHE_MAX_ENTRIES as u64],
            |row| row.get::<_, u64>(0),
        )
        .unwrap_or(DEFAULT_CACHE_MAX_ENTRIES as u64);
    let clamped = raw.clamp(1, CACHE_ENTRIES_MAX as u64);
    let entries = usize::try_from(clamped).expect("clamped to CACHE_ENTRIES_MAX");
    assert!(entries >= 1);
    assert!(entries <= CACHE_ENTRIES_MAX);
    NonZeroUsize::new(entries).expect("clamped to at least one")
}

/// Canonicalize the cache key so symlinks and relative paths map to the same
/// entry. Falls back to the raw path for non-existent files (in-memory DBs,
/// transient races during creation).
fn cache_key(db_path: &str) -> String {
    std::fs::canonicalize(db_path)
        .map(|p| p.to_string_lossy().into_owned())
        .unwrap_or_else(|_| db_path.to_string())
}

/// Index of the load lock that serializes loads of `db_path`.
// Only corkscrew-graph-tests calls this, to pick keys on distinct stripes.
#[allow(dead_code)]
pub fn load_lock_stripe(db_path: &str) -> usize {
    stripe_for_key(&cache_key(db_path))
}

fn stripe_for_key(key: &str) -> usize {
    // `DefaultHasher::new()` uses fixed keys, so a key's stripe is stable.
    let mut hasher = DefaultHasher::new();
    key.hash(&mut hasher);
    let stripe = (hasher.finish() % LOAD_LOCK_STRIPES as u64) as usize;
    assert!(stripe < LOAD_LOCK_STRIPES);
    stripe
}

/// An entry is fresh iff (a) it hasn't aged past TTL and (b) the file's
/// fingerprint still matches the one taken before the load. A `None`
/// fingerprint (in-memory DB) is matched against `None`; any divergence means
/// the file was rewritten and we need to reload.
fn entry_is_fresh(loaded: &LoadedGraph, key: &str, ttl: u64) -> bool {
    if loaded.loaded_at_instant.elapsed().as_secs() >= ttl {
        return false;
    }
    fingerprint_path(key) == loaded.file_fingerprint
}

/// Returns the cached graph for `key` if it is still fresh.
fn cached_fresh(key: &str, ttl: u64) -> Option<Arc<LoadedGraph>> {
    let mut guard = lock(cache());
    let existing = guard.get(key)?;
    entry_is_fresh(existing, key, ttl).then(|| Arc::clone(existing))
}

pub fn get_or_load(conn: &Connection, db_path: &str) -> Result<Arc<LoadedGraph>> {
    get_or_load_with(conn, db_path, |conn| {
        let providers = super::schema::detect_providers(conn)?;
        super::loader::load_graph(conn, &providers)
    })
}

/// `get_or_load` with the graph loader injected, so tests can observe and
/// interleave with a load in progress.
pub fn get_or_load_with(
    conn: &Connection,
    db_path: &str,
    load: impl FnOnce(&Connection) -> Result<LoadedGraph>,
) -> Result<Arc<LoadedGraph>> {
    let ttl = cache_ttl_secs(conn);
    let max_entries = cache_max_entries(conn);
    let key = cache_key(db_path);

    {
        let mut guard = lock(cache());
        // Resize on the fly when the session variable changes so users get the
        // configured behavior without restarting.
        if guard.cap() != max_entries {
            guard.resize(max_entries);
        }
    }
    if let Some(hit) = cached_fresh(&key, ttl) {
        return Ok(hit);
    }

    // Loads of the same key wait for each other so a burst of identical
    // queries loads once; loads of other keys proceed on their own stripes.
    let _load_guard = lock(&LOAD_LOCKS[stripe_for_key(&key)]);
    if let Some(hit) = cached_fresh(&key, ttl) {
        return Ok(hit);
    }

    // Fingerprint before loading: a rewrite during the load then leaves the
    // entry with a stale fingerprint, so it can never be served as fresh.
    let fingerprint_before = fingerprint_path(&key);
    let mut loaded = load(conn)?;
    loaded.file_fingerprint = fingerprint_before;
    let loaded = Arc::new(loaded);

    // If the file changed while loading, the graph may mix old and new data.
    // Return it to this caller, but do not cache it for anyone else.
    let fingerprint_after = fingerprint_path(&key);
    if fingerprint_after == fingerprint_before {
        lock(cache()).put(key, Arc::clone(&loaded));
    }
    Ok(loaded)
}

pub fn invalidate(db_path: &str) {
    let key = cache_key(db_path);
    lock(cache()).pop(&key);
}
