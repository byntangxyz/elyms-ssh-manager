# Design Document: elyms - TUI SSH Manager

**Date:** 2026-11-14  
**Project:** elyms  
**Author:** AI Agent & User  
**Status:** In Review  

---

## 1. Intent and The Why

Developers and system administrators frequently manage multiple remote servers, virtual machines, and cloud instances. Modern GUI tools like Termius provide a polished visual experience (saved hosts, key management, quick connection) but are resource-intensive, heavy, and require leaving the terminal. On the other hand, raw CLI `ssh` requires remembering aliases, hostnames, or manually editing `~/.ssh/config`.

**elyms** bridges this gap: a terminal-native, highly responsive, lightweight TUI (Terminal User Interface) SSH manager for Windows. It provides a Termius-like interactive workflow directly inside the terminal without consuming excessive RAM or CPU.

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
- **Lightweight & Fast:** Minimal memory footprint (< 25 MB RAM) and instant startup (< 50ms) as a single standalone executable on Windows.
- **Interactive & Intuitive TUI:** A clean, readable interface displaying saved connections, metadata preview, real-time filtering/search, and context-sensitive keybindings.
- **Standard `~/.ssh/` Integration:** Read and write hosts directly in standard `$HOME/.ssh/config`, ensuring interoperability with standard `ssh` terminal commands.
- **SSH Key Pair Management:** Generate modern cryptographic keys (Ed25519 by default, RSA optional) directly in `$HOME/.ssh/`, view public keys, and bind them to host configurations.
- **Seamless Session Execution:** Connect to hosts using the system's native `ssh.exe` via terminal suspension, providing full VT100/raw terminal emulation with 100% OpenSSH feature support (agent, ProxyJump, etc.) and restoring the TUI cleanly upon disconnect.

### Non-Goals
- **In-App Split-Screen Terminal Emulator:** elyms is an SSH connection manager, not a terminal multiplexer (like tmux or Windows Terminal panes). It does not embed an internal ANSI terminal emulator.
- **Cloud Sync / Remote Vault:** No proprietary cloud accounts, external syncing, or closed telemetry. Everything stays local in `$HOME/.ssh`.
- **SFTP / File Transfer GUI (v1):** Focus for v1 is purely on connection management, host editing, key generation, and launching SSH sessions.

### Anti-Goals
- **No Heavy Runtimes:** No Electron, Node.js, or Python runtimes. Must be a self-contained, statically linked binary.
- **No Config File Mangling:** Editing or adding a host must preserve existing comments, formatting, and unknown OpenSSH directives in `~/.ssh/config`.

---

## 3. Constraints

- **Platform Target:** Windows 10/11 (support for Windows Terminal, ConEmu, PowerShell, and cmd.exe). Path separators and home directory resolution must handle Windows user directories (`%USERPROFILE%` / `C:\Users\<Name>\.ssh`).
- **Dependencies:** Pure Go standard library + established TUI libraries (`charmbracelet/bubbletea`, `lipgloss`, `bubbles`). Must not require external C compilers (MSVC or MinGW) to build or run.
- **Environment:** Requires OpenSSH client (`ssh.exe`), which is installed by default on modern Windows.

---

## 4. Approaches Considered

### Approach 1: Hybrid — Pure Go TUI & Key Management + System `ssh.exe` Runner (Selected)
- **Description:** Pure Go handles the TUI, key generation (`crypto/ed25519`), and parsing/updating `~/.ssh/config`. Bubble Tea’s `tea.ExecProcess` suspends the TUI and delegates terminal control to system `ssh <host>`.
- **Pros:** 
  - Zero terminal emulation bugs (resizing, colors, raw PTY handled by Windows OpenSSH).
  - Native support for SSH agents, security keys, and complex configs (`ProxyJump`, `Match`).
  - Smallest maintenance burden, clean separation of concerns.
- **Cons:** Dependent on system `ssh.exe` being in PATH.
- **Verdict:** Selected by user as the most reliable, clean, and lightweight approach.

### Approach 2: Pure Go Built-in SSH Engine (`golang.org/x/crypto/ssh`)
- **Description:** All connections and terminal sessions are handled internally via Go's crypto/ssh library and a custom terminal loop.
- **Pros:** Completely self-contained without needing external `ssh.exe`.
- **Cons:** Re-implementing OpenSSH directives (e.g. ProxyJump, HostKeyAlgorithms, agent forwarding) and handling Windows ConPTY terminal resizing internally is fragile and complex.
- **Verdict:** Rejected due to high complexity and maintenance overhead.

### Approach 3: Rust + Ratatui
- **Description:** Single binary built using Rust and the Ratatui framework.
- **Pros:** Maximum performance and lowest possible RAM (< 10 MB).
- **Cons:** Complex Windows toolchain requirements (MSVC Build Tools), steep learning curve, and C-linking issues with SSH libraries on Windows.
- **Verdict:** Rejected in favor of Go.

---

## 5. Architectural & Detailed Design

### 5.1 Project Structure
```
elyms/
├── cmd/
│   └── elyms/
│       └── main.go           # Application entrypoint
├── internal/
│   ├── config/               # OpenSSH config parser and writer (~/.ssh/config)
│   │   ├── config.go
│   │   └── config_test.go
│   ├── keygen/               # SSH key generator (Ed25519 & RSA)
│   │   ├── keygen.go
│   │   └── keygen_test.go
│   ├── ssh/                  # SSH runner executing system ssh
│   │   └── runner.go
│   └── ui/                   # Bubble Tea TUI
│       ├── model.go          # Main application state machine
│       ├── styles.go         # Lipgloss themes, layout, and colors
│       ├── views/
│       │   ├── hostlist.go   # Hosts list view & details pane
│       │   ├── hostform.go   # Add/Edit host modal form
│       │   ├── keymanager.go # Key list & key generation modal
│       │   └── help.go       # Keybindings & shortcut cheatsheet
└── go.mod
```

### 5.2 Core Modules

#### A. SSH Config Handler (`internal/config`)
- Loads `$HOME/.ssh/config` (creates directory and empty file if not present).
- Parses `Host` blocks into a structured `HostEntry` model:
  - `Alias` (the identifier used in `Host <alias>`)
  - `HostName` (IP address or domain)
  - `User` (username)
  - `Port` (port number, default 22)
  - `IdentityFile` (path to private key)
  - `ExtraDirectives` (map of additional options like `ForwardAgent`, `ProxyJump`, etc., preserved verbatim)
- Serialization retains existing comments and directive ordering where possible.

#### B. SSH Key Generator (`internal/keygen`)
- Generates Ed25519 (default) or RSA (4096-bit) key pairs.
- Encodes private key in OpenSSH PEM / PKCS#8 format.
- Formats public key into standard `ssh-ed25519 <key> <comment>` format.
- Writes files to `$HOME/.ssh/<filename>` and `$HOME/.ssh/<filename>.pub`.
- Applies proper Windows permissions.

#### C. Connection Runner (`internal/ssh`)
- Uses `tea.ExecProcess` from Bubble Tea:
  ```go
  cmd := exec.Command("ssh", hostAlias)
  return tea.ExecProcess(cmd, func(err error) tea.Msg {
      return sshFinishedMsg{err}
  })
  ```
- Before execution, the terminal switches out of alternate screen buffer; upon return, Bubble Tea re-enters raw mode and redraws the UI without state loss.

#### D. TUI Layout & Interactions (`internal/ui`)
- **Theme & Aesthetic:** High-contrast, clean typography powered by Lipgloss (subtle borders, muted secondary text, distinct accent for active selections).
- **Three-Panel / Split Layout:**
  - **Left Panel (60% width):** Scrollable list of saved hosts with active selection indicator and quick search filter (`/`).
  - **Right Panel (40% width):** Detailed inspector displaying selected host attributes (HostName, User, Port, Key Path, Ping/Reachability status placeholder, extra directives).
  - **Bottom Status Bar:** Global context-aware keybindings (`Enter: Connect`, `a: Add`, `e: Edit`, `d: Delete`, `k: Keys`, `/: Search`, `q: Quit`).
- **Modal Views:**
  - **Host Form Modal:** Clean text input fields for Host, HostName, User, Port, IdentityFile (with file picker or dropdown of keys in `~/.ssh`).
  - **Key Manager Modal:** List existing keys in `~/.ssh/`, view public key string (with copy-to-clipboard shortcut), and generate a new keypair.
  - **Confirmation Dialog:** For destructive actions (e.g., deleting a host entry).

---

## 6. What's Left to the Builder

- Exact Lipgloss styling colors (defaulting to a modern Nord / Tokyo Night / Gruvbox inspired dark palette).
- Selection of the exact Go ssh config library (e.g. `kevinburke/ssh_config` vs lightweight custom parser).
- Clipboard copy library (e.g. `atotto/clipboard`) for copying public keys to the Windows clipboard.
- Unit test coverage for config parsing and keygen.
