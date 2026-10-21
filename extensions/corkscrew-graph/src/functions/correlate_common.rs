// Bounded pair walking shared by the correlators that compare records in a group
// across providers (shared IP, DNS name, secret hash, domain, and load balancer
// target). Only pairs from different providers can correlate, so a group is
// partitioned by provider and only cross-provider pairs are visited. A single
// shared value such as an RFC1918 or Docker bridge address can still hold
// thousands of records in several clouds, and the cross-provider pairs grow with
// the product of the partition sizes, so every walk here is bounded twice: by the
// pairs one group would produce, and by the rows returned in total.
//
// It also holds the row type, ordering, and output helpers every correlator shares,
// so every correlator's result set keeps one schema and one order.

use std::collections::HashMap;
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::BindInfo;
use duckdb::Connection;
use serde_json::json;

use crate::functions::common::{chunk_capacity, next_chunk};
use crate::functions::params::BoxError;

/// Most cross-provider pairs one group may produce before it is skipped. Half the
/// row cap, so one group can never fail the query on its own; real cross-provider
/// matches share a value across a handful of resources, so a group past this is a
/// shared private or NAT address seen from many resources in more than one cloud.
pub const GROUP_PAIRS_MAX: usize = 500_000;
/// Total rows one correlator may return; matches the core's one-million-row query cap.
pub const CORRELATION_ROWS_MAX: usize = 1_000_000;
// A group at the bound adds at most one row per pair, so a single group can never
// trip the total cap on its own; only many large groups together can.
const _: () = assert!(GROUP_PAIRS_MAX <= CORRELATION_ROWS_MAX);
const _: () = assert!(GROUP_PAIRS_MAX > 0);

/// `correlation_type` of the row that flags a group skipped for exceeding the bound.
pub const GROUP_SKIPPED_TYPE: &str = "group_skipped";

#[derive(Clone, Debug)]
pub struct CorrelationRow {
    pub correlation_id: String,
    pub correlation_type: String,
    pub source_id: String,
    pub target_id: String,
    pub source_provider: String,
    pub target_provider: String,
    pub confidence: f64,
    pub evidence: String,
    pub description: String,
}

/// A record that can be grouped for correlation: pairs are only formed between
/// records whose providers differ.
pub trait GroupMember {
    fn provider(&self) -> &str;
}

impl<T: GroupMember + ?Sized> GroupMember for &T {
    fn provider(&self) -> &str {
        (**self).provider()
    }
}

/// Bounds for one correlator run. Production uses `CorrelationLimits::DEFAULT`; tests
/// pass smaller values to reach both failure paths cheaply.
#[derive(Clone, Copy, Debug)]
pub struct CorrelationLimits {
    pub group_pairs_max: usize,
    pub rows_max: usize,
}

impl CorrelationLimits {
    pub const DEFAULT: CorrelationLimits = CorrelationLimits {
        group_pairs_max: GROUP_PAIRS_MAX,
        rows_max: CORRELATION_ROWS_MAX,
    };
}

/// Names one group: `kind` is what the group shares (for example `ip_match`) and
/// `value` is the shared value itself. A struct, so the two cannot be swapped.
#[derive(Clone, Copy, Debug)]
pub struct GroupKey<'a> {
    pub kind: &'a str,
    pub value: &'a str,
}

/// A group partitioned by provider: `buckets[b]` holds the ascending group indices of
/// one provider, and `bucket_of[i]` names the bucket of `group[i]`.
struct ProviderBuckets {
    buckets: Vec<Vec<usize>>,
    bucket_of: Vec<usize>,
}

/// Calls `pair` once for every pair `(group[i], group[j])` with `i < j` whose providers
/// differ, and pushes each row it returns into `rows`. Same-provider pairs are never
/// visited: they can never correlate, so they cost nothing and count against nothing.
///
/// A group that would produce more than `limits.group_pairs_max` cross-provider pairs
/// is not walked. It gets one flag row instead (see `skipped_group_row`), so the
/// omission is visible in the result rather than silent. A single-provider group has
/// no pairs and is never flagged. Returns an error once `rows` would exceed
/// `limits.rows_max`.
pub fn walk_group_pairs<T: GroupMember>(
    group: &[T],
    key: GroupKey<'_>,
    limits: CorrelationLimits,
    rows: &mut Vec<CorrelationRow>,
    mut pair: impl FnMut(&T, &T) -> Option<CorrelationRow>,
) -> Result<(), BoxError> {
    assert!(limits.group_pairs_max > 0);
    assert!(limits.rows_max > 0);
    assert!(rows.len() <= limits.rows_max);

    let partition = provider_buckets(group);
    if partition.buckets.len() < 2 {
        return Ok(());
    }
    let pair_count = cross_provider_pair_count(&partition.buckets);
    if pair_count > limits.group_pairs_max as u64 {
        let shape = GroupShape {
            record_count: group.len(),
            provider_count: partition.buckets.len(),
            pair_count,
        };
        return push_row(rows, skipped_group_row(key, shape, limits), limits);
    }

    let rows_before = rows.len();
    let mut visit_count: u64 = 0;
    for (left_index, left) in group.iter().enumerate() {
        let left_bucket = partition.bucket_of[left_index];
        for (bucket, indices) in partition.buckets.iter().enumerate() {
            if bucket == left_bucket {
                continue;
            }
            // Indices ascend, so the later records of this provider are a suffix.
            let start = indices.partition_point(|&index| index <= left_index);
            for &right_index in &indices[start..] {
                assert!(left_index < right_index);
                visit_count += 1;
                if let Some(row) = pair(left, &group[right_index]) {
                    push_row(rows, row, limits)?;
                }
            }
        }
    }
    assert_eq!(visit_count, pair_count);
    assert!((rows.len() - rows_before) as u64 <= pair_count);
    Ok(())
}

/// Partitions `group` by provider, buckets in order of first appearance.
fn provider_buckets<T: GroupMember>(group: &[T]) -> ProviderBuckets {
    let mut bucket_by_provider: HashMap<&str, usize> = HashMap::new();
    let mut buckets: Vec<Vec<usize>> = Vec::new();
    let mut bucket_of = Vec::with_capacity(group.len());
    for (index, member) in group.iter().enumerate() {
        let bucket = *bucket_by_provider
            .entry(member.provider())
            .or_insert_with(|| {
                buckets.push(Vec::new());
                buckets.len() - 1
            });
        buckets[bucket].push(index);
        bucket_of.push(bucket);
    }
    assert_eq!(bucket_of.len(), group.len());
    assert_eq!(buckets.iter().map(Vec::len).sum::<usize>(), group.len());
    ProviderBuckets { buckets, bucket_of }
}

/// Pairs drawn from two different buckets: the sum over buckets of its size times the
/// sizes of the buckets before it. Saturates, since only "past the cap" matters.
fn cross_provider_pair_count(buckets: &[Vec<usize>]) -> u64 {
    let mut pair_count: u64 = 0;
    let mut records_before: u64 = 0;
    for bucket in buckets {
        assert!(!bucket.is_empty());
        let size = bucket.len() as u64;
        pair_count = pair_count.saturating_add(size.saturating_mul(records_before));
        records_before += size;
    }
    pair_count
}

fn push_row(
    rows: &mut Vec<CorrelationRow>,
    row: CorrelationRow,
    limits: CorrelationLimits,
) -> Result<(), BoxError> {
    if rows.len() < limits.rows_max {
        rows.push(row);
        return Ok(());
    }
    Err(format!(
        "correlation result exceeds {} rows; raise min_confidence to narrow it",
        limits.rows_max
    )
    .into())
}

/// What a skipped group looked like: named fields, so the counts cannot be swapped.
#[derive(Clone, Copy, Debug)]
struct GroupShape {
    record_count: usize,
    provider_count: usize,
    pair_count: u64,
}

/// The flag row for a skipped group. It bypasses `min_confidence` (a filter must
/// never hide that data was not examined) and carries confidence 0.0, so the
/// conventional `ORDER BY confidence DESC` lists it after every real correlation.
fn skipped_group_row(
    key: GroupKey<'_>,
    shape: GroupShape,
    limits: CorrelationLimits,
) -> CorrelationRow {
    assert!(shape.pair_count > limits.group_pairs_max as u64);
    assert!(shape.provider_count >= 2);
    CorrelationRow {
        correlation_id: format!("{GROUP_SKIPPED_TYPE}:{}:{}", key.kind, key.value),
        correlation_type: GROUP_SKIPPED_TYPE.to_string(),
        source_id: key.value.to_string(),
        target_id: String::new(),
        source_provider: String::new(),
        target_provider: String::new(),
        confidence: 0.0,
        evidence: json!({
            "group_kind": key.kind,
            "group_value": key.value,
            "group_size": shape.record_count,
            "group_providers": shape.provider_count,
            "group_pairs": shape.pair_count,
            "group_pairs_max": limits.group_pairs_max,
        })
        .to_string(),
        description: format!(
            "Skipped {} group '{}': {} cross-provider pairs among {} records exceed the \
             limit of {}; no pairs were compared",
            key.kind, key.value, shape.pair_count, shape.record_count, limits.group_pairs_max
        ),
    }
}

/// True when `table_name` exists in the database, compared case-insensitively.
pub fn table_exists(conn: &Connection, table_name: &str) -> Result<bool, BoxError> {
    let mut stmt = conn.prepare(
        "SELECT COUNT(*) FROM information_schema.tables WHERE lower(table_name) = lower(?)",
    )?;
    let count: i64 = stmt.query_row([table_name], |row| row.get(0))?;
    Ok(count > 0)
}

/// Result order shared by every correlator: confidence descending, then providers
/// and ids ascending, so output is deterministic.
pub fn sort_rows(a: &CorrelationRow, b: &CorrelationRow) -> std::cmp::Ordering {
    b.confidence
        .partial_cmp(&a.confidence)
        .unwrap_or(std::cmp::Ordering::Equal)
        .then_with(|| a.source_provider.cmp(&b.source_provider))
        .then_with(|| a.target_provider.cmp(&b.target_provider))
        .then_with(|| a.source_id.cmp(&b.source_id))
        .then_with(|| a.target_id.cmp(&b.target_id))
}

/// Declares the nine result columns every correlator returns, in `CorrelationRow`
/// field order.
pub fn add_standard_columns(bind: &BindInfo) {
    for name in [
        "correlation_id",
        "correlation_type",
        "source_id",
        "target_id",
        "source_provider",
        "target_provider",
    ] {
        bind.add_result_column(name, LogicalTypeHandle::from(LogicalTypeId::Varchar));
    }
    bind.add_result_column("confidence", LogicalTypeHandle::from(LogicalTypeId::Double));
    bind.add_result_column("evidence", LogicalTypeHandle::from(LogicalTypeId::Varchar));
    bind.add_result_column(
        "description",
        LogicalTypeHandle::from(LogicalTypeId::Varchar),
    );
}

/// Writes the next chunk of `rows` into `output`, in `add_standard_columns` order.
pub fn emit_rows(
    cursor: &AtomicUsize,
    rows: &[CorrelationRow],
    output: &mut DataChunkHandle,
) -> Result<(), BoxError> {
    let Some((start, end)) = next_chunk(cursor, rows.len(), chunk_capacity()) else {
        output.set_len(0);
        return Ok(());
    };
    let v0 = output.flat_vector(0);
    let v1 = output.flat_vector(1);
    let v2 = output.flat_vector(2);
    let v3 = output.flat_vector(3);
    let v4 = output.flat_vector(4);
    let v5 = output.flat_vector(5);
    let mut v6 = output.flat_vector(6);
    let v7 = output.flat_vector(7);
    let v8 = output.flat_vector(8);
    let confidence = v6.as_mut_slice::<f64>();
    for (out_idx, row) in rows[start..end].iter().enumerate() {
        v0.insert(out_idx, CString::new(row.correlation_id.clone())?);
        v1.insert(out_idx, CString::new(row.correlation_type.clone())?);
        v2.insert(out_idx, CString::new(row.source_id.clone())?);
        v3.insert(out_idx, CString::new(row.target_id.clone())?);
        v4.insert(out_idx, CString::new(row.source_provider.clone())?);
        v5.insert(out_idx, CString::new(row.target_provider.clone())?);
        confidence[out_idx] = row.confidence;
        v7.insert(out_idx, CString::new(row.evidence.clone())?);
        v8.insert(out_idx, CString::new(row.description.clone())?);
    }
    output.set_len(end - start);
    Ok(())
}

/// Orders a (source, target) pair by (provider, id) so each pair has one
/// canonical direction; the flag is true when the pair was swapped.
pub fn ordered_values(
    source_id: String,
    target_id: String,
    source_provider: String,
    target_provider: String,
) -> (String, String, String, String, bool) {
    if (&source_provider, &source_id) <= (&target_provider, &target_id) {
        (
            source_id,
            target_id,
            source_provider,
            target_provider,
            false,
        )
    } else {
        (target_id, source_id, target_provider, source_provider, true)
    }
}

/// Trims `value`, mapping a missing or blank string to `None`.
pub fn clean_string(value: Option<String>) -> Option<String> {
    value
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty())
}
