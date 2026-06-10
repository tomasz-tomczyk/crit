# Database Migration Plan

## Overview

This document outlines the migration from PostgreSQL 14 to PostgreSQL 16.
The migration window is Saturday 2am-6am UTC to minimize user impact.

## Pre-Migration Checklist

- [ ] Take full database backup
- [ ] Verify backup can be restored to test environment
- [ ] Run migration dry-run on staging
- [ ] Notify all teams of maintenance window
- [ ] Prepare rollback scripts

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
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Data Migration Steps

1. Create new tables alongside old ones
2. Backfill `users.mfa_enabled` from the feature_flags table
3. Migrate session data from JSON blobs to sessions_v2
4. Verify row counts match between old and new tables
5. Swap table names in a single transaction
6. Drop old tables after 7-day soak period

## Rollback Plan

If anything goes wrong during the migration window:

1. Stop the application immediately
2. Rename tables back to their original names
3. Restore from backup if any data corruption is detected
4. Restart application with the old schema compatibility flag
5. Page the on-call DBA for post-mortem

## Performance Considerations

The audit_log table will grow rapidly. Plan for:

- Partition by month on `created_at`
- Retain 90 days online, archive older partitions to S3
- Add indexes on `(actor_id, created_at)` and `(resource_type, resource_id)`

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
