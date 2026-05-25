use duckdb::Result;
use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{
    PathRow, chunk_capacity, collect_shortest_path_rows, load_graph_for_path, next_chunk,
    write_optional_varchar,
};

#[repr(C)]
pub struct PathsBindData {
    pub db_path: String,
    pub from_id: String,
    pub to_id: String,
    pub weighted: bool,
}

pub struct PathsInitData {
    cursor: AtomicUsize,
    rows: Vec<PathRow>,
}

pub struct GraphShortestPathVTab;

impl VTab for GraphShortestPathVTab {
    type InitData = PathsInitData;
    type BindData = PathsBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        bind.add_result_column("hop_number", LogicalTypeHandle::from(LogicalTypeId::Integer));
        bind.add_result_column("resource_id", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("resource_type", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("resource_name", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("region", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("incoming_rel_type", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("total_hops", LogicalTypeHandle::from(LogicalTypeId::Integer));

        Ok(PathsBindData {
            db_path: bind.get_parameter(0).to_string(),
            from_id: bind.get_parameter(1).to_string(),
            to_id: bind.get_parameter(2).to_string(),
            weighted: parse_bool(&bind.get_parameter(3).to_string())?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe { init.get_bind_data::<PathsBindData>().as_ref().unwrap() };
        let loaded = load_graph_for_path(&bind_ref.db_path)?;
        let rows = collect_shortest_path_rows(&loaded, &bind_ref.from_id, &bind_ref.to_id, bind_ref.weighted);

        Ok(PathsInitData {
            cursor: AtomicUsize::new(0),
            rows,
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), Box<dyn std::error::Error>> {
        let init_data = func.get_init_data();
        let Some((start, end)) = next_chunk(&init_data.cursor, init_data.rows.len(), chunk_capacity()) else {
            output.set_len(0);
            return Ok(());
        };

        let slice = &init_data.rows[start..end];
        let mut v0 = output.flat_vector(0);
        let v1 = output.flat_vector(1);
        let v2 = output.flat_vector(2);
        let v3 = output.flat_vector(3);
        let v4 = output.flat_vector(4);
        let mut v5 = output.flat_vector(5);
        let mut v6 = output.flat_vector(6);
        let hop_slice = v0.as_mut_slice::<i32>();
        let total_slice = v6.as_mut_slice::<i32>();

        for (index, row) in slice.iter().enumerate() {
            hop_slice[index] = row.hop_number;
            v1.insert(index, &row.resource_id);
            v2.insert(index, &row.resource_type);
            v3.insert(index, &row.resource_name);
            v4.insert(index, &row.region);
            write_optional_varchar(&mut v5, index, &row.incoming_rel_type);
            total_slice[index] = row.total_hops;
        }

        output.set_len(slice.len());
        Ok(())
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Boolean),
        ])
    }
}

fn parse_bool(value: &str) -> Result<bool, Box<dyn std::error::Error>> {
    match value.to_ascii_lowercase().as_str() {
        "true" | "1" => Ok(true),
        "false" | "0" => Ok(false),
        _ => Err(format!("invalid boolean '{value}'").into()),
    }
}
