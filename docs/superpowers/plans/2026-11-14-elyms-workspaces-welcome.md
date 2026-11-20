# elyms Workspaces and Welcome Inspector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional TUI-managed workspaces with persistent host assignments and a useful right-pane welcome panel when no host is selected.

**Architecture:** Keep OpenSSH config as the source for connection settings and add a separate versioned JSON metadata file for workspace names and alias assignments. Extend the existing host list with workspace filtering and section headings; wire workspace management through the root Bubble Tea model and retain the existing inspector for selected hosts.

**Tech Stack:** Go 1.25, Bubble Tea, Lipgloss, standard library JSON and filesystem APIs.

**Spec:** `docs/superpowers/specs/2026-11-14-elyms-workspaces-welcome-design.md`

## Global Constraints

- A host has at most one workspace assignment.
- Workspace data is stored separately from `%USERPROFILE%\.ssh\config`.
- **All** shows named workspace sections followed by **Unassigned**.
- Search only filters the current workspace context.
- Removing a workspace leaves its hosts in SSH config and makes them unassigned.
- Workspace UI changes must not change existing host connection, editing, or key-management behavior.
- Welcome panel credits **byntangxyz** and is displayed when no host is selected.

## Review Focus

- Malformed, future-version, or partially written metadata must not corrupt or rewrite SSH config; test loading failures and atomic writes.
- Host aliases missing from config after an external edit must not crash or be connected as stale workspace entries; test reconciliation after reload.
- Pattern hosts such as `Host *` must stay visible and non-connectable without accidental workspace assignment; test in All and named views.
- Workspace labels containing whitespace, punctuation, duplicate case variants, or control characters must be safely handled; test validation and uniqueness.
- Narrow/minimum-size terminals must keep workspace navigation, section headers, welcome content, and status hints within bounds; test row/width limits.

---

### Task 1: Workspace Metadata Store

**Files:**
- Create: `internal/workspace/store.go`
- Create: `internal/workspace/store_test.go`

**Interfaces:**
- Produces `workspace.Store`, `workspace.Workspace`, `workspace.Load(path, aliases)`, `(*Store).Save()`, `(*Store).Assign(alias, name)`, `(*Store).Unassign(alias)`, `(*Store).Delete(name)`, and `(*Store).Rename(old, new)`.
- Metadata JSON schema: version integer `1`, ordered workspace-name array, alias-to-workspace string map.
- Use atomic temp-file-and-rename saving; metadata path is `%USERPROFILE%\\.elyms\\workspaces.json` (derived with `os.UserConfigDir()` and an `elyms` subdirectory if that is the platform's config directory).

- [ ] **Step 1: Write failing metadata tests** for missing file defaults, round-trip persistence/order, duplicate/invalid workspace names, unknown assignments, workspace deletion unassigning rather than deleting hosts, rename preserving assignments, and malformed/unsupported version errors.
- [ ] **Step 2: Run `go test ./internal/workspace -count=1`** and confirm the tests fail because the package API does not exist.
- [ ] **Step 3: Implement metadata types and API** in `internal/workspace/store.go`; validate workspace names as trimmed non-empty labels without control characters, require case-insensitive uniqueness, and ignore assignments whose aliases are absent from the supplied host alias set.
- [ ] **Step 4: Add write-failure coverage** proving a failed save leaves the previous metadata file intact; use temp directories and a deterministic filesystem failure fixture.
- [ ] **Step 5: Run `go test ./internal/workspace -count=1`** and confirm all store tests pass.

### Task 2: Workspace Context and Grouped Host List

**Files:**
- Modify: `internal/ui/views/hostlist.go`
- Modify: `internal/ui/views/hostlist_test.go`
- Create: `internal/ui/views/workspaces.go`
- Create: `internal/ui/views/workspaces_test.go`

**Interfaces:**
- Consumes Task 1's `workspace.Store` and `workspace.Workspace`.
- Produces view messages `WorkspaceCreateMsg{Name string}`, `WorkspaceRenameMsg{OldName, NewName string}`, `WorkspaceDeleteMsg{Name string}`, `WorkspaceSelectMsg{Name string}`, and `WorkspaceAssignMsg{Alias, Name string}`; empty `Name` in assignment means unassign.
- `HostList.SetWorkspaceData(names []string, assignments map[string]string)` and `HostList.SetWorkspace(name string)` control grouping/filter context.

- [ ] **Step 1: Add failing list tests** asserting All renders workspace sections in metadata order followed by Unassigned, host order within sections follows SSH config order, named context hides hosts from other workspaces, and search only filters the current context.
- [ ] **Step 2: Run `go test ./internal/ui/views -run 'Workspace|Group|Search' -count=1`** and confirm failures.
- [ ] **Step 3: Implement grouping and filter scope** in `HostList`; preserve selection by alias when possible and prevent section labels from being treated as selectable hosts.
- [ ] **Step 4: Add failing modal tests** for workspace select/create/rename/delete and host assign/unassign choices, cancel behavior, and minimum terminal dimensions.
- [ ] **Step 5: Implement workspace modal views** in `workspaces.go` with explicit selectable entries and typed messages; do not perform persistence inside view components.
- [ ] **Step 6: Run `go test ./internal/ui/views -count=1`** and confirm all view tests pass, including existing host list tests.

### Task 3: Root Model Persistence and TUI Actions

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`
- Modify: `cmd/elyms/main.go`

**Interfaces:**
- Consumes Task 1 metadata store API and Task 2 typed view messages.
- `Options` gains `WorkspacePath string`; `New` loads workspace metadata and supplies current config aliases for reconciliation.
- Root key bindings: `w` opens workspace selection/management; `g` assigns the selected host. Modal choices provide create, rename, delete, switch, assign, and unassign actions.

- [ ] **Step 1: Add failing model tests** for `w` and `g` actions, switching active context, creating/renaming/deleting workspaces, assigning/unassigning selected hosts, persistence failures surfaced as error status without changing the prior state, and config reload reconciliation.
- [ ] **Step 2: Run `go test ./internal/ui -run 'Workspace|Assign' -count=1`** and confirm failures.
- [ ] **Step 3: Add workspace path resolution** in `cmd/elyms/main.go` using the OS user config directory and pass it through `ui.Options`.
- [ ] **Step 4: Wire modal messages into the root model**; mutate a candidate store, save successfully before applying it to the visible model, and refresh list grouping while preserving selected alias.
- [ ] **Step 5: Ensure removing a workspace only clears assignments** and never calls SSH config mutation APIs; test that SSH config bytes remain unchanged after workspace-only operations.
- [ ] **Step 6: Run `go test ./internal/ui ./internal/workspace -count=1`** and confirm model/store tests pass.

### Task 4: Welcome Inspector and Discoverability

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`
- Modify: `internal/ui/views/hostlist.go`
- Modify: `internal/ui/views/hostlist_test.go`

**Interfaces:**
- Reuses the current `viewMain` split pane and inspector path.
- Adds a welcome view function in `hostlist.go` or a focused `welcome.go` view file; exact function signature: `Welcome(width, height int) string`.

- [ ] **Step 1: Add failing welcome tests** asserting title, short purpose text, **byntangxyz**, add-host/key/help hints, standard `.ssh/config` note, and no content overflow at 50 columns and the supported minimum height.
- [ ] **Step 2: Run `go test ./internal/ui/views -run Welcome -count=1`** and confirm failure.
- [ ] **Step 3: Implement `Welcome(width, height int) string`** using existing styles and ANSI-aware clipping.
- [ ] **Step 4: Wire the welcome panel when the selected host is nil**; selected host behavior must continue using `Inspector` unchanged.
- [ ] **Step 5: Add failing hint-line tests** for workspace selector and assignment controls in normal and filtering states; implement concise hints that remain within terminal width.
- [ ] **Step 6: Run `go test ./internal/ui/... -count=1`** and confirm all UI tests pass.

### Task 5: Full Verification and User Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/ledger.md` (internal, ignored from public Git)

- [ ] **Step 1: Update README** to document workspace behavior, the metadata path, key bindings, and the rule that workspace operations do not modify `.ssh/config`.
- [ ] **Step 2: Run `go test ./...`** and confirm every package passes.
- [ ] **Step 3: Run `go vet ./...`** and confirm no diagnostics.
- [ ] **Step 4: Run `go build -trimpath -ldflags "-s -w" -o elyms.exe ./cmd/elyms`** and confirm a Windows executable is produced.
- [ ] **Step 5: Manually verify** All grouping, named-workspace search, workspace CRUD/assignment, welcome panel, SSH connection and unchanged SSH config at 50x16 terminal size; record any environment-limited checks in the ledger.

## Self-review

- Spec coverage: metadata safety and lifecycle in Task 1; grouping/search in Task 2; persistence and management controls in Task 3; welcome and hints in Task 4; compatibility and docs in Task 5.
- Review Focus coverage: malformed/failed metadata in Task 1; stale aliases in Tasks 1 and 3; patterns in Task 2; labels in Task 1; narrow terminal output in Tasks 2 and 4.
- Existing SSH config behavior is explicitly protected by a byte-preservation assertion in Task 3.
