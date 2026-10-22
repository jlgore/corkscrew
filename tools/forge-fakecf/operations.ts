// The Cloudflare API operations the fake serves: every call the cloudflare provider makes, as
// `METHOD path` exactly as the Forge spec writes the path. The plugin fails if one is missing
// from the spec, and the fake fails a test that sends a request matching none of them.
export const OPERATIONS: readonly string[] = [
  // accounts, zones, dns
  'GET /accounts',
  'GET /accounts/{account_id}',
  'GET /zones',
  'GET /zones/{zone_id}',
  'GET /zones/{zone_id}/dns_records',
  'GET /zones/{zone_id}/dns_records/{dns_record_id}',

  // workers
  'GET /accounts/{account_id}/workers/scripts',
  'GET /accounts/{account_id}/workers/scripts/{script_name}/settings',
  'GET /accounts/{account_id}/workers/scripts/{script_name}/subdomain',
  'GET /accounts/{account_id}/workers/scripts/{script_name}/secrets',
  'GET /accounts/{account_id}/workers/domains',
  'GET /accounts/{account_id}/workers/domains/{domain_id}',
  'GET /zones/{zone_id}/workers/routes',
  'GET /zones/{zone_id}/workers/routes/{route_id}',

  // storage
  'GET /accounts/{account_id}/r2/buckets',
  'GET /accounts/{account_id}/r2/buckets/{bucket_name}',
  'GET /accounts/{account_id}/storage/kv/namespaces',
  'GET /accounts/{account_id}/storage/kv/namespaces/{namespace_id}',
  'GET /accounts/{account_id}/queues',
  'GET /accounts/{account_id}/queues/{queue_id}',

  // data
  'GET /accounts/{account_id}/d1/database',
  'GET /accounts/{account_id}/d1/database/{database_id}',
  'GET /accounts/{account_id}/workers/durable_objects/namespaces',
  'GET /accounts/{account_id}/workers/durable_objects/namespaces/{id}/objects',
  'GET /accounts/{account_id}/secrets_store/stores',
  'GET /accounts/{account_id}/secrets_store/stores/{store_id}/secrets',
  'GET /accounts/{account_id}/secrets_store/stores/{store_id}/secrets/{secret_id}',
];
