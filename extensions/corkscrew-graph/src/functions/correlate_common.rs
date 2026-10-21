// Bounded pair walking shared by the correlators that compare every record in a
// group with every other record (shared IP, DNS name, secret hash, domain, and
// load balancer target). A single shared value such as an RFC1918 or Docker
// bridge address can hold thousands of records, and the pair loop is O(n²) in
// the group size, so every walk here is bounded twice: per group and in total.

use serde_json::json;

use crate::functions::params::BoxError;

/// Largest group whose pairs are correlated. Real cross-provider matches share a value
/// across a handful of resources; a group past 1024 is a shared private or NAT address.
pub const GROUP_SIZE_MAX: usize = 1024;
/// Total rows one correlator may return; matches the core's one-million-row query cap.
pub const CORRELATION_ROWS_MAX: usize = 1_000_000;
// One group at the bound yields at most n(n-1)/2 pairs, so a single group can never
// trip the total cap on its own; only many large groups together can.
const _: () = assert!(GROUP_SIZE_MAX * (GROUP_SIZE_MAX - 1) / 2 < CORRELATION_ROWS_MAX);

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

/// Bounds for one correlator run. Production uses `CorrelationLimits::DEFAULT`; tests
/// pass smaller values to reach both failure paths cheaply.
#[derive(Clone, Copy, Debug)]
pub struct CorrelationLimits {
    pub group_size_max: usize,
    pub rows_max: usize,
}

impl CorrelationLimits {
    pub const DEFAULT: CorrelationLimits = CorrelationLimits {
        group_size_max: GROUP_SIZE_MAX,
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

/// Calls `pair` once for every unordered pair `(group[i], group[j])` with `i < j` and
/// pushes each row it returns into `rows`.
///
/// A group larger than `limits.group_size_max` is not walked. It gets one flag row
/// instead (see `skipped_group_row`), so the omission is visible in the result
/// rather than silent. Returns an error once `rows` would exceed `limits.rows_max`.
pub fn walk_group_pairs<T>(
    group: &[T],
    key: GroupKey<'_>,
    limits: CorrelationLimits,
    rows: &mut Vec<CorrelationRow>,
    mut pair: impl FnMut(&T, &T) -> Option<CorrelationRow>,
) -> Result<(), BoxError> {
    assert!(
        limits.group_size_max >= 2,
        "a pair needs a group of at least two"
    );
    assert!(limits.rows_max > 0);
    assert!(rows.len() <= limits.rows_max);

    if group.len() > limits.group_size_max {
        return push_row(rows, skipped_group_row(key, group.len(), limits), limits);
    }

    let rows_before = rows.len();
    for i in 0..group.len() {
        for j in (i + 1)..group.len() {
            if let Some(row) = pair(&group[i], &group[j]) {
                push_row(rows, row, limits)?;
            }
        }
    }
    // A group within the bound adds at most one row per pair.
    assert!(rows.len() - rows_before <= group.len() * group.len().saturating_sub(1) / 2);
    Ok(())
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

/// The flag row for a skipped group. It bypasses `min_confidence` (a filter must
/// never hide that data was not examined) and carries confidence 0.0, so the
/// conventional `ORDER BY confidence DESC` lists it after every real correlation.
fn skipped_group_row(
    key: GroupKey<'_>,
    group_size: usize,
    limits: CorrelationLimits,
) -> CorrelationRow {
    assert!(group_size > limits.group_size_max);
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
            "group_size": group_size,
            "group_size_max": limits.group_size_max,
        })
        .to_string(),
        description: format!(
            "Skipped {} group '{}': {} records exceed the limit of {}; no pairs were compared",
            key.kind, key.value, group_size, limits.group_size_max
        ),
    }
}
