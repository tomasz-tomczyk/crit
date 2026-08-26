# Roundtrip integration tests

The roundtrip suite exercises `crit pull` and `crit push` against real change requests on both supported forges. Both providers' tests live in `internal/session` and drive the compiled Crit CLI; provider APIs are used only for fixture lifecycle and remote-state assertions.

| Provider | Build tag | Project variable | CLI | Runner |
| --- | --- | --- | --- | --- |
| GitHub | `e2e_github` | `CRIT_ROUNDTRIP_REPO` | `gh` | `make e2e-roundtrip` |
| GitLab | `e2e_gitlab` | `CRIT_GITLAB_ROUNDTRIP_PROJECT` | `glab` | `make e2e-gitlab-roundtrip` |

Each scenario:

1. Creates a unique branch and opens a PR or MR against the sandbox's initialized default branch.
2. Drives public `crit comment`, `crit pull`, and `crit push` commands through a state transition.
3. Asserts both local review-file state and live provider comment/thread state.
4. Closes the change request and deletes the temporary branch in `t.Cleanup`.

## One-time setup

Use throwaway repositories named `<owner>/crit-roundtrip-sandbox`. The shared defaults are `crit-md/crit-roundtrip-sandbox` on both providers.

- GitHub: export `CRIT_ROUNDTRIP_REPO=<owner>/crit-roundtrip-sandbox` and confirm `gh auth status`. The harness clones over SSH by default; set `CRIT_ROUNDTRIP_CLONE_URL` to override it.
- GitLab: export `CRIT_GITLAB_ROUNDTRIP_PROJECT=<owner>/crit-roundtrip-sandbox` and confirm `glab auth status`. The authenticated user must be able to push branches, create/close MRs, comment, and resolve discussions. The project must have an initialized default branch. For self-managed GitLab, also set `CRIT_GITLAB_ROUNDTRIP_HOST=gitlab.example.com` and configure Crit's `gitlab_url` for that host.

## Running

```bash
make e2e-roundtrip
make e2e-gitlab-roundtrip

./scripts/e2e-roundtrip.sh -run TestRoundtrip_PushIsIdempotent -v
./scripts/e2e-gitlab-roundtrip.sh -run TestGitLabRoundtrip_FullCommentLifecycle -v
```

## Adding a scenario

- GitHub scenarios use `roundtrip_integration_test.go` and `roundtrip_helpers_test.go`; start with `e := newRoundtripEnv(t)`.
- GitLab scenarios use `gitlab_roundtrip_integration_test.go`; start with `e := newGitLabRoundtripEnv(t)`.
- Keep provider API calls in harness helpers. Exercise Crit behavior through the compiled binary rather than calling provider implementation functions directly.
- Assert remote IDs, thread/reply structure, resolution, deletion, and repeated pull/push idempotency where applicable.

## Caveats

- These tests are intentionally excluded from default `go test ./...`: they are slow, networked, rate-limited, and mutate sandbox repositories.
- Closed PR/MR history remains in the sandboxes, but temporary branches and open change requests should not remain after cleanup.
- If a process is killed before `t.Cleanup`, inspect the sandbox for branches named `rt-*` or `crit-gitlab-e2e-*`.
