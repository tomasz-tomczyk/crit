# Database Migration Plan

## Overview

This document outlines the migration from PostgreSQL 14 to PostgreSQL 16.
The migration window is Saturday 2am-6am UTC to minimize user impact.

## Scope

This migration covers the primary application database only. Read replicas
will be updated automatically via streaming replication. The analytics
warehouse (Redshift) is out of scope and will be handled separately.

## Pre-Migration Checklist

- [ ] Take full database backup
- [ ] Verify backup can be restored to test environment
- [ ] Run migration dry-run on staging
- [ ] Notify all teams of maintenance window
- [ ] Prepare rollback scripts
- [ ] Confirm connection pool settings for PG16 compatibility
- [ ] Update pg_dump and pg_restore to version 16 binaries

## Schema Changes

### Users Table

Add new columns for the auth refactor:

| Column | Type | Nullable | Default | Notes |
| ------ | ---- | -------- | ------- | ----- |
| mfa_enabled | boolean | NO | false | Two-factor auth flag |
| last_login_at | timestamptz | YES | NULL | Track login recency |
| login_count | integer | NO | 0 | For analytics |
| password_changed_at | timestamptz | YES | NULL | Password rotation |

### Sessions Table

The sessions table needs a complete rewrite. The current schema stores session
data as a JSON blob which prevents indexing. The new schema normalizes the data:

```sql
CREATE TABLE sessions_v2 (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id BIGINT NOT NULL REFERENCES users(id),
  token_hash BYTEA NOT NULL,
  ip_address INET,
  user_agent TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ
);
```

### Audit Log

New table for compliance requirements. Legal has flagged that we need a full
audit trail for all user-facing mutations by end of quarter.

```sql
CREATE TABLE audit_log (
  id BIGSERIAL PRIMARY KEY,
  actor_id BIGINT REFERENCES users(id),
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT,
  metadata JSONB DEFAULT '{}',
  ip_address INET,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Data Migration Steps

1. Create new tables alongside old ones
2. Backfill `users.mfa_enabled` from the feature_flags table
3. Migrate session data from JSON blobs to sessions_v2
4. Verify row counts match between old and new tables
5. Run foreign key consistency checks across both schemas
6. Swap table names in a single transaction
7. Drop old tables after 7-day soak period

## Performance Considerations

The audit_log table will grow to approximately 50M rows per month. Mitigation:

- Partition by month using declarative partitioning on `created_at`
- Create a partition pruning policy: 90 days hot, then archive to S3 via pg_dump
- Composite indexes: `(actor_id, created_at)` for user lookups, `(resource_type, resource_id)` for resource audit trails
- Enable `pg_stat_statements` to monitor query performance post-migration

## Monitoring

The migration worker emits structured JSON logs for every step. Key metrics:

- Migration throughput: rows/second per table
- Lock wait time during the swap transaction
- Replication lag on read replicas after swap

## Timeline

| Phase | Description | Duration | Owner |
| ----- | ----------- | -------- | ----- |
| 1 | Schema creation on staging | 1 day | Backend |
| 2 | Data migration scripts | 3 days | Backend |
| 3 | Staging validation | 2 days | QA |
| 4 | Production migration | 4 hours | SRE |
| 5 | Post-migration monitoring | 3 days | SRE |

## Risks

- Large table migration may cause brief read lock during the swap transaction
- Session migration could miss currently active sessions if they refresh mid-swap
- Audit log backfill from application logs may have gaps for requests before logging was added
- PG16 breaking changes in jsonpath operators may affect existing reporting queries
