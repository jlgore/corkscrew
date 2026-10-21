use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::Result;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{
    chunk_capacity, collect_traverse_rows, load_graph_for_path, next_chunk,
    write_varchar_list_column, TraversalDirection, TraverseRow, TRAVERSE_ROWS_MAX,
};
use crate::functions::params::{
    bind_data, guard, param_hops, param_optional_varchar, param_varchar, BoxError,
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

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphTraverseVTab::bind", || {
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

            let db_path = param_varchar(bind, 0, "db_path")?;
            let source_id = param_varchar(bind, 1, "source_id")?;
            let max_hops = param_hops(bind, 2, "max_hops")?;
            let direction = TraversalDirection::parse(&param_varchar(bind, 3, "direction")?)?;
            let filter_type = param_optional_varchar(bind, 4);

            Ok(TraverseBindData {
                db_path,
                source_id,
                max_hops,
                direction,
                filter_type,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphTraverseVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let loaded = load_graph_for_path(&bind_ref.db_path)?;
            let rows = collect_traverse_rows(
                &loaded,
                &bind_ref.source_id,
                bind_ref.max_hops,
                bind_ref.direction,
                bind_ref.filter_type.as_deref(),
                TRAVERSE_ROWS_MAX,
            )?;

            Ok(TraverseInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphTraverseVTab::func", || {
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
        })
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
