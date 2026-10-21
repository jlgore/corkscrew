use duckdb::core::{DataChunkHandle, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::Result;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{
    chunk_capacity, collect_reachable_row, load_graph_for_path, next_chunk, write_optional_varchar,
    ReachableRow,
};
use crate::functions::params::{bind_data, guard, param_hops, param_varchar, BoxError};

#[repr(C)]
pub struct ReachableBindData {
    pub db_path: String,
    pub source_id: String,
    pub target_type: String,
    pub max_hops: usize,
}

pub struct ReachableInitData {
    cursor: AtomicUsize,
    row: ReachableRow,
}

pub struct GraphReachableVTab;

impl VTab for GraphReachableVTab {
    type InitData = ReachableInitData;
    type BindData = ReachableBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphReachableVTab::bind", || {
            bind.add_result_column(
                "is_reachable",
                LogicalTypeHandle::from(LogicalTypeId::Boolean),
            );
            bind.add_result_column(
                "match_count",
                LogicalTypeHandle::from(LogicalTypeId::Bigint),
            );
            bind.add_result_column(
                "closest_hop",
                LogicalTypeHandle::from(LogicalTypeId::Integer),
            );
            bind.add_result_column(
                "example_id",
                LogicalTypeHandle::from(LogicalTypeId::Varchar),
            );

            Ok(ReachableBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                source_id: param_varchar(bind, 1, "source_id")?,
                target_type: param_varchar(bind, 2, "target_type")?,
                max_hops: param_hops(bind, 3, "max_hops")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphReachableVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let loaded = load_graph_for_path(&bind_ref.db_path)?;
            Ok(ReachableInitData {
                cursor: AtomicUsize::new(0),
                row: collect_reachable_row(
                    &loaded,
                    &bind_ref.source_id,
                    &bind_ref.target_type,
                    bind_ref.max_hops,
                ),
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphReachableVTab::func", || {
            let init_data = func.get_init_data();
            let Some((_, end)) = next_chunk(&init_data.cursor, 1, chunk_capacity()) else {
                output.set_len(0);
                return Ok(());
            };

            let mut v0 = output.flat_vector(0);
            let mut v1 = output.flat_vector(1);
            let mut v2 = output.flat_vector(2);
            let mut v3 = output.flat_vector(3);

            v0.as_mut_slice::<bool>()[0] = init_data.row.is_reachable;
            v1.as_mut_slice::<i64>()[0] = init_data.row.match_count;
            match init_data.row.closest_hop {
                Some(value) => v2.as_mut_slice::<i32>()[0] = value,
                None => v2.set_null(0),
            }
            write_optional_varchar(&mut v3, 0, &init_data.row.example_id);
            output.set_len(end);
            Ok(())
        })
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Integer),
        ])
    }
}
