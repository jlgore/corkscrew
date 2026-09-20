# ADR 0011: One Hub serves one trust domain

Status: Accepted

DuckDB and Quack do not provide the authorization isolation required to treat rows in one database as secure tenants. A Corkscrew Hub therefore represents one trust domain and may aggregate many accounts, clusters, repositories, and agents whose operators are allowed to share a security graph. Mutually untrusted organizations or teams deploy separate Hubs rather than relying on a `workspace_id` discriminator.

Asset identity includes its source namespace and native account, cluster, or repository identity. Cross-Hub federation, if added, is a read/query concern and does not merge independently administered storage or credentials.
