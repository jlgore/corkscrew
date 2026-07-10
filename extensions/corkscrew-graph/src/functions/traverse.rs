use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::ffi::duckdb_is_null_value;
use duckdb::vtab::Value;
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::Result;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{
    chunk_capacity, collect_traverse_rows, load_graph_for_path, next_chunk,
    write_varchar_list_column, TraversalDirection, TraverseRow,
};

#[repr(C)]
pub struct TraverseBindData {
    pub db_path: String,
    pub source_id: String,
    pub max_hops: usize,
    pub direction: TraversalDirection,
    pub filter_type: Option<String>,
}

pub struct TraverseInitData {
    cursor: AtomicUsize,
    rows: Vec<TraverseRow>,
}

pub struct GraphTraverseVTab;

impl VTab for GraphTraverseVTab {
    type InitData = TraverseInitData;
    type BindData = TraverseBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        bind.add_result_column("node_id", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("node_type", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("node_name", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("region", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column(
            "account_id",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );
        bind.add_result_column("provider", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("hop_count", LogicalTypeHandle::from(LogicalTypeId::Integer));
        bind.add_result_column(
            "path_ids",
            LogicalTypeHandle::list(&LogicalTypeHandle::from(LogicalTypeId::Varchar)),
        );
        bind.add_result_column(
            "relationship_types",
            LogicalTypeHandle::list(&LogicalTypeHandle::from(LogicalTypeId::Varchar)),
        );

        let db_path = bind.get_parameter(0).to_string();
        let source_id = bind.get_parameter(1).to_string();
        let max_hops = bind.get_parameter(2).to_string().parse::<usize>()?;
        let direction = TraversalDirection::parse(&bind.get_parameter(3).to_string())?;
        let filter_type = optional_varchar(&bind.get_parameter(4));

        Ok(TraverseBindData {
            db_path,
            source_id,
            max_hops,
            direction,
            filter_type,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe { init.get_bind_data::<TraverseBindData>().as_ref().unwrap() };
        let loaded = load_graph_for_path(&bind_ref.db_path)?;
        let rows = collect_traverse_rows(
            &loaded,
            &bind_ref.source_id,
            bind_ref.max_hops,
            bind_ref.direction,
            bind_ref.filter_type.as_deref(),
        );

        Ok(TraverseInitData {
            cursor: AtomicUsize::new(0),
            rows,
        })
    }

    fn func(
        func: &TableFunctionInfo<Self>,
        output: &mut DataChunkHandle,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let init_data = func.get_init_data();
        let Some((start, end)) =
            next_chunk(&init_data.cursor, init_data.rows.len(), chunk_capacity())
        else {
            output.set_len(0);
            return Ok(());
        };

        let slice = &init_data.rows[start..end];
        let len = slice.len();
        let v0 = output.flat_vector(0);
        let v1 = output.flat_vector(1);
        let v2 = output.flat_vector(2);
        let v3 = output.flat_vector(3);
        let v4 = output.flat_vector(4);
        let v5 = output.flat_vector(5);
        let mut v6 = output.flat_vector(6);
        let mut v7 = output.list_vector(7);
        let mut v8 = output.list_vector(8);
        let hop_slice = v6.as_mut_slice::<i32>();

        let mut path_ids = Vec::with_capacity(len);
        let mut relationship_types = Vec::with_capacity(len);
        for (index, row) in slice.iter().enumerate() {
            v0.insert(index, &row.node_id);
            v1.insert(index, &row.node_type);
            v2.insert(index, &row.node_name);
            v3.insert(index, &row.region);
            v4.insert(index, &row.account_id);
            v5.insert(index, &row.provider);
            hop_slice[index] = row.hop_count;
            path_ids.push(row.path_ids.clone());
            relationship_types.push(row.relationship_types.clone());
        }

        write_varchar_list_column(&mut v7, &path_ids)?;
        write_varchar_list_column(&mut v8, &relationship_types)?;
        output.set_len(len);
        Ok(())
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Integer),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        ])
    }
}

fn optional_varchar(value: &Value) -> Option<String> {
    if unsafe { duckdb_is_null_value(raw_duckdb_value(value)) } {
        return None;
    }

    let value = value.to_string();
    (!value.eq_ignore_ascii_case("NULL") && !value.is_empty()).then_some(value)
}

// Static guard: if duckdb-rs ever changes `Value`'s layout we want a build
// failure, not silent UB. `Value` is currently a `#[repr(transparent)]`-shaped
// newtype around `duckdb_value` — if either side adds fields, this assert fires.
// TODO: remove the raw cast workaround once duckdb-rs exposes a public
// `is_null` helper for `Value` (the Display impl aborts on NULL today).
const _: () = assert!(
    std::mem::size_of::<Value>() == std::mem::size_of::<duckdb::ffi::duckdb_value>(),
    "duckdb::vtab::Value layout changed; raw_duckdb_value cast is unsound — see traverse.rs"
);

unsafe fn raw_duckdb_value(value: &Value) -> duckdb::ffi::duckdb_value {
    *(value as *const Value).cast::<duckdb::ffi::duckdb_value>()
}
