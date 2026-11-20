# Design Document: elyms Workspaces and Welcome Inspector

**Date:** 2026-11-14  
**Project:** elyms  
**Author:** byntangxyz  
**Status:** Awaiting approval

## Intent

Make elyms more useful for people managing several SSH contexts, such as a NOC, personal infrastructure, staging, and production, without changing the standard OpenSSH config format. Improve first-use UX by showing a useful landing panel in the right-hand pane before a host is selected.

## Goals

- Let users create, rename, and remove optional workspaces from the TUI.
- Assign each SSH host to zero or one workspace.
- Keep `%USERPROFILE%\\.ssh\\config` as the source of connection settings and leave it unchanged when only workspace data changes.
- Show an **All** view grouped by workspace, with unassigned hosts under **Unassigned**.
- Show all hosts in the selected workspace view, grouped consistently with the active workspace.
- Scope host search to the active workspace; search in **All** searches all hosts.
- Show a right-pane welcome panel when no host is selected, explaining elyms, crediting **byntangxyz**, and listing essential controls.
- Preserve existing host editing, key management, and direct `ssh.exe` behavior.

## Non-goals

- Splitting hosts into separate SSH config files.
- Writing workspace labels into SSH config comments.
- Syncing workspace metadata to a server or cloud service.
- Allowing a host to belong to multiple workspaces in this version.
- Replacing the current host inspector when a host is selected.

## Approaches considered

### Separate elyms metadata (selected)

Store workspace names and host-to-workspace assignments in an elyms-owned metadata file beside the normal SSH data, while keeping SSH connection fields in `~/.ssh/config`. This avoids surprising other SSH tools and lets workspace operations be independent of config editing.

### SSH config comments

Store markers beside `Host` blocks. This avoids a second data file but modifies a shared file and introduces parsing, preservation, and conflict risks. Rejected.

### Separate config per workspace

Give every workspace its own SSH config. This gives strong isolation but complicates existing configs, host movement, and direct OpenSSH interoperability. Rejected.

## Proposed behavior

### Workspace state

- The workspace selector has an **All** state plus named workspaces.
- A workspace name is unique and validated like a user-facing label.
- A host has at most one workspace assignment.
- Removing a workspace does not delete hosts; its hosts become **Unassigned**.
- Removing a host also removes its metadata assignment when the next save/reconciliation occurs.
- Missing hosts in metadata are ignored or cleaned up without changing SSH config.
- Metadata write failures are shown as TUI errors and do not partially change in-memory state.

### Host list

- **All:** sections for named workspaces, then **Unassigned**.
- Named workspace: show the hosts assigned to that workspace, with the workspace context visible in the header.
- Within each section, preserve SSH config order.
- The existing pattern entries such as `Host *` remain visible but are not connectable and are not assigned by default.
- Search filters the current view only.

### Controls

The final key names should follow existing elyms conventions, with discoverability in the hint bar. The design expects controls for:

- Switching workspace context.
- Creating, renaming, and removing a workspace.
- Assigning the selected host to a workspace or clearing its assignment.

These controls should be available without requiring users to edit the metadata file manually.

### Welcome panel

When no host is selected, the right pane shows:

- `elyms` and “TUI SSH manager”.
- A short explanation that elyms manages OpenSSH hosts and keys locally.
- `byntangxyz` as author/maintainer credit.
- A short “Getting started” section: add a host, move with arrows/j/k, press Enter to connect, and press `k` for keys.
- A note that connection settings remain in the standard `.ssh/config`.

The panel is an empty-selection state, not a modal, so it disappears naturally when a host is selected.

## Persistence

Introduce a small, versioned metadata representation owned by elyms. The implementation should use a stable path under the user’s SSH/application data area and should write atomically with a backup or recovery strategy consistent with the existing config safety expectations. The exact filename and serialization format are implementation decisions for the plan, but the format must be human-readable and must not contain private key material.

## Verification plan

- Unit tests for loading, saving, malformed metadata, duplicate workspace names, assignment cleanup, workspace deletion, and atomic failure behavior.
- UI tests for All/workspace grouping, search scoping, assignment flows, workspace management, and the welcome panel.
- Existing config, keygen, SSH, UI, `go test ./...`, and `go vet ./...` suites remain passing.
- Manual TUI check at the minimum supported terminal size and with an existing SSH config containing wildcard hosts, comments, and unmanaged directives.

## Open implementation choices

- Exact metadata path and file format.
- Exact key bindings for workspace navigation and management.
- Whether workspace headers are collapsible or simply rendered as non-selectable sections.
- Whether the active workspace is selected from a top bar, a dedicated workspace modal, or a compact selector in the list header.
