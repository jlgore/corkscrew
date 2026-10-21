use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{
    core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId},
    Result,
};
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};
use crate::functions::params::{bind_data, guard, param_varchar, BoxError};
// We'll implement cache invalidation as a simple table function (VTab)
// to avoid pulling in the vscalar/libduckdb_sys runtime initialization.

// Bind data stores the user-provided parameter (db_path) captured during bind.
#[repr(C)]
pub struct InfoBindData {
    pub db_path: String,
}

pub struct InfoInitData {
    cursor: AtomicUsize,
    rows: Vec<(String, i64, i64, String)>,
}

pub struct GraphInfoVTab;

impl VTab for GraphInfoVTab {
    type InitData = InfoInitData;
    type BindData = InfoBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphInfoVTab::bind", || {
            // Result columns
            bind.add_result_column("provider", LogicalTypeHandle::from(LogicalTypeId::Varchar));
            bind.add_result_column("nodes", LogicalTypeHandle::from(LogicalTypeId::Bigint));
            bind.add_result_column("edges", LogicalTypeHandle::from(LogicalTypeId::Bigint));
            bind.add_result_column("loaded_at", LogicalTypeHandle::from(LogicalTypeId::Varchar));

            // Expect a single VARCHAR parameter: db_path
            let db_path = param_varchar(bind, 0, "db_path")?;
            Ok(InfoBindData { db_path })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphInfoVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let db_path = &bind_ref.db_path;

            let conn = duckdb::Connection::open(db_path)?;
            let loaded = crate::graph::cache::get_or_load(&conn, db_path)?;

            let loaded_at = loaded.loaded_at.to_rfc3339();
            let rows = loaded
                .provider_counts
                .iter()
                .map(|(provider, (nodes, edges))| {
                    (provider.clone(), *nodes, *edges, loaded_at.clone())
                })
                .collect();

            Ok(InfoInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphInfoVTab::func", || {
            let init_data = func.get_init_data();
            let Some((start, end)) =
                next_chunk(&init_data.cursor, init_data.rows.len(), chunk_capacity())
            else {
                output.set_len(0);
                return Ok(());
            };

            let v0 = output.flat_vector(0);
            let mut v1 = output.flat_vector(1);
            let mut v2 = output.flat_vector(2);
            let v3 = output.flat_vector(3);
            let nodes_slice = v1.as_mut_slice::<i64>();
            let edges_slice = v2.as_mut_slice::<i64>();

            for (out_idx, (prov, nodes, edges, loaded_at)) in
                init_data.rows[start..end].iter().enumerate()
            {
                v0.insert(out_idx, CString::new(prov.clone())?);
                nodes_slice[out_idx] = *nodes;
                edges_slice[out_idx] = *edges;
                v3.insert(out_idx, CString::new(loaded_at.clone())?);
            }
            output.set_len(end - start);
            Ok(())
        })
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![LogicalTypeHandle::from(LogicalTypeId::Varchar)])
    }
}

// If we want a scalar cache invalidation function later, implement it as a
// VScalar with the vscalar feature enabled and using libduckdb_sys types.
// GraphCacheInvalidate implemented as a table function that accepts a single
// VARCHAR parameter (db_path) and returns a single-row BIGINT (1) on success.
#[repr(C)]
pub struct CacheInvalidateBindData {
    pub db_path: String,
}

pub struct CacheInvalidateInitData {
    cursor: AtomicUsize,
}

/// Invalidate the graph cache for the given db_path. Returns 1 on success.
pub fn graph_cache_invalidate(db_path: &str) -> i64 {
    crate::graph::cache::invalidate(db_path);
    1
}

pub struct GraphCacheInvalidateVTab;

impl VTab for GraphCacheInvalidateVTab {
    type InitData = CacheInvalidateInitData;
    type BindData = CacheInvalidateBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCacheInvalidateVTab::bind", || {
            bind.add_result_column(
                "invalidated",
                LogicalTypeHandle::from(LogicalTypeId::Bigint),
            );
            Ok(CacheInvalidateBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCacheInvalidateVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            graph_cache_invalidate(&bind_ref.db_path);
            Ok(CacheInvalidateInitData {
                cursor: AtomicUsize::new(0),
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCacheInvalidateVTab::func", || {
            let init_data = func.get_init_data();
            let Some((start, end)) = next_chunk(&init_data.cursor, 1, chunk_capacity()) else {
                output.set_len(0);
                return Ok(());
            };

            let mut vector = output.flat_vector(0);
            vector.as_mut_slice::<i64>()[0] = 1;
            output.set_len(end - start);
            Ok(())
        })
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![LogicalTypeHandle::from(LogicalTypeId::Varchar)])
    }
}
