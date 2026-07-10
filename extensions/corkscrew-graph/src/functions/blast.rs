use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::Result;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{
    chunk_capacity, collect_blast_rows, load_graph_for_path, next_chunk, write_varchar_list_column,
    BlastRow,
};

#[repr(C)]
pub struct BlastBindData {
    pub db_path: String,
    pub source_id: String,
    pub max_hops: usize,
}

pub struct BlastInitData {
    cursor: AtomicUsize,
    rows: Vec<BlastRow>,
}

pub struct GraphBlastRadiusVTab;

impl VTab for GraphBlastRadiusVTab {
    type InitData = BlastInitData;
    type BindData = BlastBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        bind.add_result_column(
            "resource_type",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );
        bind.add_result_column(
            "reachable_count",
            LogicalTypeHandle::from(LogicalTypeId::Integer),
        );
        bind.add_result_column(
            "max_hop_distance",
            LogicalTypeHandle::from(LogicalTypeId::Integer),
        );
        bind.add_result_column(
            "sample_ids",
            LogicalTypeHandle::list(&LogicalTypeHandle::from(LogicalTypeId::Varchar)),
        );

        Ok(BlastBindData {
            db_path: bind.get_parameter(0).to_string(),
            source_id: bind.get_parameter(1).to_string(),
            max_hops: bind.get_parameter(2).to_string().parse::<usize>()?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe { init.get_bind_data::<BlastBindData>().as_ref().unwrap() };
        let loaded = load_graph_for_path(&bind_ref.db_path)?;
        let rows = collect_blast_rows(&loaded, &bind_ref.source_id, bind_ref.max_hops);

        Ok(BlastInitData {
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
        let mut v1 = output.flat_vector(1);
        let mut v2 = output.flat_vector(2);
        let mut v3 = output.list_vector(3);
        let reach_slice = v1.as_mut_slice::<i32>();
        let hop_slice = v2.as_mut_slice::<i32>();
        let mut sample_ids = Vec::with_capacity(len);

        for (index, row) in slice.iter().enumerate() {
            v0.insert(index, &row.resource_type);
            reach_slice[index] = row.reachable_count;
            hop_slice[index] = row.max_hop_distance;
            sample_ids.push(row.sample_ids.clone());
        }

        write_varchar_list_column(&mut v3, &sample_ids)?;
        output.set_len(len);
        Ok(())
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Integer),
        ])
    }
}
