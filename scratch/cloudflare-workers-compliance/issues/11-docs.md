# Docs

Status: ready-for-agent
Depends on: 07, 08, 09

- `plugins/cloudflare-provider/README.md`: add `worker_binding` to the coverage table. Document the
  binding model and the rule that Corkscrew never stores binding values, the per-script request
  cost, and how to run the live test.
- A pack reference page that lists each control, its parameters, and their defaults.
- Make sure `corkscrew cloudflare auth plan --services workers` still lists every scope the
  new calls need.
