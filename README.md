# elyms

**elyms** is a lightweight Windows TUI SSH manager for people who prefer working in the terminal.

It provides a Termius-like host list and key manager while continuing to use the standard Windows OpenSSH configuration and `ssh.exe`.

> **Status:** Early public release. The core host, key, and connection workflows are implemented. The Windows installer is planned and will be documented here when available.

## Features

- Browse and search hosts from `%USERPROFILE%\.ssh\config`
- Add, edit, rename, and delete host entries
- Preserve existing comments, unknown directives, formatting, line endings, and UTF-8 BOMs when editing the config
- Detect duplicate host aliases before allowing risky edits
- Connect through the system OpenSSH client with `ssh -- <alias>`
- Generate Ed25519 keys by default or RSA 4096 keys
- List keys and copy public keys to the clipboard
- Apply Windows ACL protection to generated private keys
- Create a `.bak` backup before the first config save in a process
- Single native Windows executable with no Node.js, Python, or C toolchain required at runtime

## Requirements

- Windows 10 or Windows 11
- Windows OpenSSH Client (`ssh.exe`)
- A terminal such as Windows Terminal, PowerShell, or Command Prompt

Check whether OpenSSH is available:

```powershell
ssh -V
```

If `ssh` is not found, install **OpenSSH Client** from **Settings → Apps → Optional features → View features**.

## Quick start

Download `elyms.exe` from the project's Releases page, then run it from PowerShell:

```powershell
.\elyms.exe
```

You can also install it into a directory on your `PATH` and then run:

```powershell
elyms
```

The installer is not included yet. Installation instructions will be added when the Windows installer is released.

## Basic controls

| Key | Action |
| --- | --- |
| `Enter` | Connect to the selected host |
| `a` | Add a host |
| `e` | Edit a host |
| `d` | Delete a host |
| `k` | Open the key manager |
| `/` | Search hosts |
| `q` | Quit |
| `Esc` | Cancel or go back |

## Add an SSH server

### 1. Generate a key

Open elyms and press:

1. `k` to open **Keys**
2. `n` to generate a key
3. Enter a name such as `id_my_server`
4. Keep `ed25519` selected and press `Enter`

The key pair is stored in:

```text
C:\Users\<your-user>\.ssh\id_my_server
C:\Users\<your-user>\.ssh\id_my_server.pub
```

Only the public key should be copied to a server. Never share the private key.

### 2. Install the public key on the server

Copy the public key from elyms with `c`, then use an existing login method to access the server:

```powershell
ssh user@server
```

On the server, append the public key as one line in `~/.ssh/authorized_keys`:

```bash
mkdir -p ~/.ssh
chmod 700 ~/.ssh
nano ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
```

### 3. Add the host in elyms

Press `a` and enter the server's:

- Alias, for example `my-server`
- Hostname or IP address
- Username
- SSH port, usually `22`
- Generated identity file

Select the host and press `Enter` to connect.

You can test the key directly before using elyms:

```powershell
ssh -i "$HOME\.ssh\id_my_server" user@server
```

## Configuration and data

elyms uses the standard OpenSSH locations:

```text
%USERPROFILE%\.ssh\config
%USERPROFILE%\.ssh\<private-key>
%USERPROFILE%\.ssh\<private-key>.pub
```

elyms does not sync data to a server or cloud service. It does not embed a terminal emulator; sessions are handed to the installed Windows OpenSSH client and returned to the TUI when they finish.

Before modifying an existing config, elyms creates a `.bak` backup beside it. Existing SSH configuration and keys are not replaced by the application installer.

## Build from source

### Requirements

- Go 1.25 or later
- Windows

Build and test with the local Go toolchain:

```powershell
$env:GOTOOLCHAIN = "local"
go test ./...
go vet ./...
go build -trimpath -ldflags "-s -w" -o elyms.exe ./cmd/elyms
```

Run the result:

```powershell
.\elyms.exe
```

## Roadmap

- Windows installer with Start Menu shortcut and optional `PATH` setup
- Release binaries and checksums
- More polished first-run guidance
- Additional OpenSSH configuration editing support

## Security notes

- Keep private keys private and protect them with appropriate permissions.
- Verify the server host key when connecting for the first time.
- Review changes to `%USERPROFILE%\.ssh\config` before using them in production.
- elyms does not collect telemetry or require an account.

## Author

Created and maintained by **byntangxyz**.

## License

elyms is licensed under the MIT License. See [LICENSE](LICENSE).
