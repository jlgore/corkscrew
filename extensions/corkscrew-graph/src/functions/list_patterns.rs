use duckdb::Result;
use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

pub struct ListPatternsBindData;

pub struct ListPatternsInitData {
    cursor: AtomicUsize,
    rows: Vec<(String, String, i32, i32)>,
}

pub struct GraphListPatternsVTab;

impl VTab for GraphListPatternsVTab {
    type InitData = ListPatternsInitData;
    type BindData = ListPatternsBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        bind.add_result_column("pattern_name", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("description", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("node_count", LogicalTypeHandle::from(LogicalTypeId::Integer));
        bind.add_result_column("edge_count", LogicalTypeHandle::from(LogicalTypeId::Integer));
        Ok(ListPatternsBindData)
    }

    fn init(_: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let rows = crate::patterns::builtin::list()
            .into_iter()
            .map(|pattern| {
                (
                    pattern.name,
                    pattern.description,
                    pattern.nodes.len() as i32,
                    pattern.edges.len() as i32,
                )
            })
            .collect();

        Ok(ListPatternsInitData {
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
        let v0 = output.flat_vector(0);
        let v1 = output.flat_vector(1);
        let mut v2 = output.flat_vector(2);
        let mut v3 = output.flat_vector(3);
        let node_slice = v2.as_mut_slice::<i32>();
        let edge_slice = v3.as_mut_slice::<i32>();

        for (index, (name, description, node_count, edge_count)) in slice.iter().enumerate() {
            v0.insert(index, name);
            v1.insert(index, description);
            node_slice[index] = *node_count;
            edge_slice[index] = *edge_count;
        }

        output.set_len(slice.len());
        Ok(())
    }
}
