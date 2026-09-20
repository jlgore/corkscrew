# ADR 0016: An operator installation owns Hub selection

Status: Accepted

One operator installation is configured with one Hub endpoint and ingestion Secret. Scan policies, scan runs, and ingest sources do not carry arbitrary Hub endpoints or token references; the run controller injects installation-level Hub configuration into execution Jobs, and runs record the Hub installation identity for provenance.

This keeps security intent separate from storage routing and prevents accidental cross-Hub submission. A second independently administered Hub is served by another operator installation with a non-overlapping watch scope.
