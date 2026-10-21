// Checked access to table-function parameters, bind data, and panic
// containment for vtab callbacks.
//
// duckdb-rs's `extern "C"` vtab trampolines do not `catch_unwind`, so a panic
// in bind/init/func aborts the host process. `Value`'s `Display` impl also
// dereferences the varchar pointer DuckDB returns for a SQL NULL, so
// `get_parameter(n).to_string()` on NULL is undefined behaviour. Everything in
// this module turns those cases into ordinary `Err`s that DuckDB reports as a
// SQL error.

use duckdb::ffi::{duckdb_get_bool, duckdb_get_double, duckdb_get_int64, duckdb_is_null_value};
use duckdb::vtab::{BindInfo, InitInfo, VTab, Value};
use std::panic::{catch_unwind, AssertUnwindSafe};

pub type BoxError = Box<dyn std::error::Error>;

/// Upper bound for every `max_hops` parameter. Hop counts are emitted as SQL
/// INTEGER, so the bound must fit in `i32`; it is far above any real cloud
/// graph's diameter, and BFS stops at the visited set long before it matters.
pub const HOPS_MAX: usize = 4096;
const _: () = assert!(HOPS_MAX <= i32::MAX as usize);

// Static guard: `raw_duckdb_value` reinterprets `&Value` as its only field.
// If duckdb-rs ever adds fields to `Value`, this fails the build instead of
// silently reading the wrong bytes.
// TODO: drop the raw cast once duckdb-rs exposes a public `is_null` on `Value`.
const _: () = assert!(
    std::mem::size_of::<Value>() == std::mem::size_of::<duckdb::ffi::duckdb_value>(),
    "duckdb::vtab::Value layout changed; raw_duckdb_value cast is unsound"
);

fn raw_duckdb_value(value: &Value) -> duckdb::ffi::duckdb_value {
    // SAFETY: `Value` is a struct whose single field is a `duckdb_value`
    // (checked by the size assert above), so the field sits at offset 0 and a
    // `*const Value` is a valid, aligned `*const duckdb_value`.
    unsafe { *(value as *const Value).cast::<duckdb::ffi::duckdb_value>() }
}

fn is_null(value: &Value) -> bool {
    // SAFETY: `value` owns a live handle from `BindInfo::get_parameter` (it is
    // destroyed only when `value` drops), and NULL checks read no payload.
    unsafe { duckdb_is_null_value(raw_duckdb_value(value)) }
}

/// Fetches parameter `index`, rejecting SQL NULL so later reads never touch a
/// NULL value's payload.
fn non_null(bind: &BindInfo, index: u64, name: &str) -> Result<Value, BoxError> {
    let value = bind.get_parameter(index);
    if is_null(&value) {
        return Err(format!("parameter {name} (position {index}) must not be NULL").into());
    }
    Ok(value)
}

pub fn param_varchar(bind: &BindInfo, index: u64, name: &str) -> Result<String, BoxError> {
    Ok(non_null(bind, index, name)?.to_string())
}

/// Reads an optional VARCHAR. SQL NULL, the empty string, and the literal
/// string 'NULL' all mean "not set"; the literal is kept for CLI callers that
/// cannot express a real NULL.
pub fn param_optional_varchar(bind: &BindInfo, index: u64) -> Option<String> {
    let value = bind.get_parameter(index);
    if is_null(&value) {
        return None;
    }
    let value = value.to_string();
    (!value.eq_ignore_ascii_case("NULL") && !value.is_empty()).then_some(value)
}

pub fn param_i64(bind: &BindInfo, index: u64, name: &str) -> Result<i64, BoxError> {
    let value = non_null(bind, index, name)?;
    // SAFETY: `value` is a live, non-NULL handle; DuckDB casts it to BIGINT.
    Ok(unsafe { duckdb_get_int64(raw_duckdb_value(&value)) })
}

/// Reads a DOUBLE, rejecting NaN and infinities: NaN survives `clamp` and
/// compares false against every threshold, which silently disables filters.
pub fn param_f64(bind: &BindInfo, index: u64, name: &str) -> Result<f64, BoxError> {
    let value = non_null(bind, index, name)?;
    // SAFETY: `value` is a live, non-NULL handle; DuckDB casts it to DOUBLE.
    let number = unsafe { duckdb_get_double(raw_duckdb_value(&value)) };
    if !number.is_finite() {
        return Err(format!("parameter {name} (position {index}) must be finite").into());
    }
    Ok(number)
}

pub fn param_bool(bind: &BindInfo, index: u64, name: &str) -> Result<bool, BoxError> {
    let value = non_null(bind, index, name)?;
    // SAFETY: `value` is a live, non-NULL handle; DuckDB casts it to BOOLEAN.
    Ok(unsafe { duckdb_get_bool(raw_duckdb_value(&value)) })
}

/// Reads a hop limit in `0..=HOPS_MAX`.
pub fn param_hops(bind: &BindInfo, index: u64, name: &str) -> Result<usize, BoxError> {
    let hops = param_i64(bind, index, name)?;
    match usize::try_from(hops) {
        Ok(hops) if hops <= HOPS_MAX => Ok(hops),
        _ => Err(format!("parameter {name} must be in 0..={HOPS_MAX}, got {hops}").into()),
    }
}

/// Converts a BFS depth to the SQL INTEGER hop column. Callers bound depth by
/// `max_hops`, which bind limits to `HOPS_MAX`, so saturation is unreachable
/// from SQL; it only keeps direct Rust callers from wrapping.
pub fn hops_to_i32(hops: usize) -> i32 {
    debug_assert!(hops <= HOPS_MAX);
    i32::try_from(hops).unwrap_or(i32::MAX)
}

/// Returns the bind data for vtab `V`. Taking `V` (not a free `T`) ties the
/// pointer cast to the type the bind trampoline actually boxed.
pub fn bind_data<V: VTab>(init: &InitInfo) -> Result<&V::BindData, BoxError> {
    let pointer = init.get_bind_data::<V::BindData>();
    // SAFETY: duckdb-rs's bind trampoline stores `Box<V::BindData>` for this
    // function and DuckDB frees it only after every init/func call has
    // finished, so the pointer is either NULL or valid for `init`'s lifetime.
    unsafe { pointer.as_ref() }.ok_or_else(|| "missing bind data".into())
}

/// Runs a vtab callback body and converts a panic into an error. This is
/// defense in depth: a panic must never unwind into DuckDB's C frames, which
/// aborts the process. Any partially built state is dropped with the closure,
/// and the shared graph cache recovers from lock poisoning.
pub fn guard<T>(callback: &str, body: impl FnOnce() -> Result<T, BoxError>) -> Result<T, BoxError> {
    match catch_unwind(AssertUnwindSafe(body)) {
        Ok(result) => result.map_err(sanitize),
        Err(payload) => {
            let detail = payload
                .downcast_ref::<&str>()
                .map(|text| (*text).to_string())
                .or_else(|| payload.downcast_ref::<String>().cloned())
                .unwrap_or_else(|| "non-string panic payload".to_string());
            Err(sanitize(format!("{callback} panicked: {detail}").into()))
        }
    }
}

/// duckdb-rs's `set_error` does `CString::new(msg).unwrap()`, which panics
/// outside our guard if the message contains NUL (for example a user-supplied
/// path echoed back in an error). Escape NUL before handing the error over.
fn sanitize(error: BoxError) -> BoxError {
    let message = error.to_string();
    if message.contains('\0') {
        return message.replace('\0', "\\0").into();
    }
    error
}
