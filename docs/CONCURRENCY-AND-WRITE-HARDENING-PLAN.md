# CONCURRENCY-AND-WRITE-HARDENING-PLAN.md

## 1. Purpose

This document defines a detailed implementation plan to harden write behavior and high-concurrency handling for database execution APIs.

Scope includes:
- SQLite runtime tuning
- Write serialization strategy
- Transaction boundaries and atomicity
- SQL request guardrails
- Optional optimistic locking for managed endpoints
- Observability and rollout/testing strategy

Scope excludes (for now):
- Rate limiting implementation (kept in-memory currently; can be moved later)

---

## 2. Current Baseline

### 2.1 Relevant Files
- `src/app/api/db/[name]/exec/route.ts`
- `src/lib/db-pool.ts`
- `src/lib/registry.ts`
- `src/app/api/metrics/` (existing metrics endpoint area)

### 2.2 Current Behavior Summary
- DB connections are pooled by name in `src/lib/db-pool.ts`.
- RW connections already use:
  - `PRAGMA journal_mode = WAL`
  - `PRAGMA busy_timeout = 5000`
- `/api/db/[name]/exec` accepts arbitrary SQL and bindings.
- Query execution path:
  - `stmt.reader === true` -> `stmt.all()` and return rows/headers
  - otherwise `stmt.run()` and return changes/lastInsertRowid
- Write blocking on inactive DB currently uses regex heuristic.

### 2.3 Risk Areas
- Short bursts of concurrent writes can still produce contention (`SQLITE_BUSY`) and tail latency spikes.
- No explicit write queue per DB currently.
- Transactions are implicit per statement but not structured for multi-step server operations.
- Large or pathological SQL payloads can impact stability.
- Lost updates possible in managed endpoints without version checks.

---

## 3. Design Goals

1. Improve write stability under burst load without breaking existing API contract.
2. Preserve read concurrency (WAL strengths).
3. Reduce `SQLITE_BUSY` and lock-wait spikes.
4. Keep migration low-risk and incremental.
5. Add observability for contention hotspots.

Non-goals:
- No immediate distributed/global consistency guarantee.
- No immediate change to client API shape unless optional future endpoint is introduced.

---

## 4. Implementation Plan (Phased)

## Phase 1: Runtime and Write Path Hardening

### 4.1 SQLite PRAGMA Tuning

Update RW connection setup in `src/lib/db-pool.ts`.

Target pragmas:
- `journal_mode = WAL` (existing)
- `busy_timeout = 10000` (increase from 5000)
- `synchronous = NORMAL`
- `wal_autocheckpoint = 1000`
- optional: `temp_store = MEMORY`

Expected effect:
- Better tolerance to lock contention and fewer immediate lock failures.
- Better write throughput characteristics while keeping durability appropriate for WAL.

### 4.2 Add Per-DB Write Queue

Create `src/lib/write-queue.ts`.

Behavior:
- One queue per database name.
- Queue concurrency = 1 for write operations only.
- Read operations continue direct path (no queuing).

Why:
- Prevent many writers from fighting each other at once.
- Smooth write bursts and reduce `SQLITE_BUSY` frequency.

Queue requirements:
- In-process only (matches current deployment assumptions).
- Configurable max queue length (protect memory).
- Optional timeout per queued operation.
- Emit queue wait metric.

### 4.3 Integrate Queue into `/exec`

Modify `src/app/api/db/[name]/exec/route.ts`:
- Determine if statement is write or read.
- Route writes through per-DB queue.
- Keep response JSON schema unchanged.

Compatibility:
- No client changes required for existing single SQL request model.

---

## Phase 2: Transaction Utilities and Guardrails

### 4.4 Add Transaction Helper Utilities

Extend `src/lib/db-pool.ts` with helpers:
- `runInTransaction(name, fn)`
- `runImmediateTransaction(name, fn)` (for lock-first semantics where needed)

Use cases:
- Multi-step server operations (future and existing paths beyond single SQL).
- Ensure all-or-nothing behavior with explicit rollback semantics.

Notes:
- Single `stmt.run()` is already atomic, but helper is still valuable for composed operations.

### 4.5 SQL Classification and Safety Guardrails

In `src/app/api/db/[name]/exec/route.ts`, replace regex-only write heuristic with stricter classifier.

Classifier outcomes:
- read
- write-dml
- write-ddl
- pragma-read
- pragma-write
- unknown

Guardrails to add:
- Max SQL text length.
- Max bindings count.
- Optional max rows returned for unbounded read.
- Stricter inactive DB write protection using classifier, not only regex.

Expected effect:
- Lower risk from pathological payloads.
- Better policy enforcement and predictable behavior.

---

## Phase 3: Observability + Conflict Protection

### 4.6 Add Execution Metrics

Add structured measurements in `src/app/api/db/[name]/exec/route.ts` and expose/aggregate through existing metrics surface.

Recommended metrics:
- `exec_requests_total{db, type=read|write}`
- `exec_queue_wait_ms`
- `exec_duration_ms`
- `exec_rows_read`
- `exec_rows_affected`
- `exec_sqlite_busy_total`
- `exec_error_total{kind}`

Log context fields:
- db name
- request type
- queue wait
- execution time
- statement prefix/classification

### 4.7 Optional Optimistic Locking in Managed APIs

Apply only where app controls schema and endpoint semantics (not generic arbitrary SQL endpoint by default).

Pattern:
- Add `version INTEGER NOT NULL DEFAULT 1` to selected tables.
- Update statements must include `WHERE id = ? AND version = ?`.
- On successful update: `version = version + 1`.
- If `changes === 0`, return `409 Conflict`.

Expected effect:
- Prevent silent lost updates in concurrent edit scenarios.

---

## 5. Suggested File-Level Change Map

### 5.1 `src/lib/db-pool.ts`
- Update PRAGMAs for RW connections.
- Add transaction helper APIs.
- Keep public API backward compatible.

### 5.2 `src/lib/write-queue.ts` (new)
- Queue registry keyed by DB name.
- Enqueue helper for writes.
- Optional queue stats helper.

### 5.3 `src/app/api/db/[name]/exec/route.ts`
- Add SQL classifier and request guardrails.
- Route writes through queue.
- Add timing/metrics instrumentation.
- Preserve response schema for compatibility.

### 5.4 `src/app/api/metrics/*`
- Add/extend counters and histograms needed for lock/contention visibility.

### 5.5 Managed API endpoints (future targeted files)
- Add optimistic-locking flow where endpoint semantics are controlled.

---

## 6. API Compatibility Strategy

### 6.1 No Breaking Changes (Default)
- Keep existing request shape:
  - `{ sql: string, bindings?: unknown[] }`
- Keep existing success/error response keys.

### 6.2 Optional Future Additions
- Batch transaction endpoint for multi-statement atomic operations:
  - Example shape: `{ statements: [{ sql, bindings }], mode: "transaction" }`
- This should be additive and optional, not replacing current endpoint.

---

## 7. Rollout Plan

### Stage A (low risk)
- PRAGMA tuning
- Write queue introduction
- `/exec` write path integration

Validation gates:
- No contract break in existing client calls.
- Reduced `SQLITE_BUSY` under synthetic write burst.

### Stage B (medium risk)
- SQL classifier and payload guardrails
- Metrics enrichment

Validation gates:
- False-positive/false-negative classifier checks.
- SLO dashboards populated.

### Stage C (targeted)
- Optimistic locking in selected managed endpoints

Validation gates:
- Conflict handling verified (`409`) and retriable client flow documented.

---

## 8. Test Plan

### 8.1 Unit Tests
- SQL classifier correctness across representative statements.
- Guardrail checks (SQL length, bindings count, inactive write block).
- Write queue behavior (serialization, timeout, queue overflow).
- Transaction helper rollback semantics.

### 8.2 Integration Tests
- Concurrent writes to same DB: ensure stability and lower busy error rate.
- Mixed read/write burst: verify reads stay responsive.
- Inactive DB write attempts blocked correctly.
- Regression checks for existing `/exec` response schema.

### 8.3 Load/Soak Checks
- Burst test (short high QPS write wave).
- Sustained mixed traffic test.
- Compare p50/p95/p99 latency and error rates before/after each phase.

---

## 9. Operational Considerations

### 9.1 In-Process Queue Caveat
- Queue state is per instance/process.
- On multi-instance deployments, serialization is per instance, not global.
- This is acceptable for current phase; monitor and revisit if scaling out heavily.

### 9.2 Failure Behavior
- Queue timeout should return clear server error (`503` or `429` policy-defined).
- Avoid unbounded queue growth; enforce max length.

### 9.3 Monitoring Alerts
- Alert on sustained queue wait growth.
- Alert on `SQLITE_BUSY` spikes.
- Alert on rising write error ratio.

---

## 10. Rate Limiting Note (Current Decision)

Current decision:
- Keep rate limiter in-memory for now.

Future path:
1. In-memory (now)
2. File-based SQLite (later, preferably per-service or sharded files)
3. Shared store (Redis or equivalent) for distributed consistency at scale

This document intentionally excludes detailed rate-limiter implementation work from current hardening phases.

---

## 11. Definition of Done

Phase 1 done when:
- PRAGMA updates deployed.
- Write queue enabled for `/exec` write path.
- No API contract changes needed by clients.

Phase 2 done when:
- Classifier + guardrails active with tests.
- Metrics expose queue and execution behavior.

Phase 3 done when:
- At least one managed endpoint ships optimistic locking with `409` conflict handling.

Final success indicators:
- Lower `SQLITE_BUSY` rate under burst traffic.
- Improved write p95/p99 latency stability.
- No regressions in read correctness or existing client behavior.
