# SDD ledger — plan: docs/superpowers/plans/2026-11-14-elyms-implementation.md

Pre-flight: config.HostEntry consumed by ui views; keygen.KeyInfo consumed by keymanager; ssh runner consumed by model. Signatures per plan.
Task 1: Ruling: pinned go.mod to go 1.25.0 with x/crypto v0.51.0, x/sys v0.46.0, x/text v0.41.0, bubbles v0.21.0 — latest versions require Go 1.26 but installed toolchain is 1.25.6 — cost if wrong: slightly older deps, no feature impact. (Use GOTOOLCHAIN=local when running go.)
Task 2: Ruling: config API is a *Config object (Load/Hosts/Has/AddOrUpdate/Delete/Save/String) plus ValidateAlias, not the plan's free functions — edits must operate on raw lines to preserve comments (Review Focus 1) — cost if wrong: none, ui consumes this API.
Task 2: complete (tests: go test ./internal/config -> ok, 15 tests)
Task 3: Ruling: keygen API takes the ssh dir explicitly — GenerateKey(dir, name, type, comment), ListKeys(dir) — so tests use temp dirs and never touch the real ~/.ssh — cost if wrong: none.
Task 3: Ruling: added restrictPrivate (Windows ACL via x/sys/windows, chmod elsewhere) — Go's 0600 is a no-op on Windows and ssh.exe rejected the key ("bad permissions") — found by a real ssh-keygen -y test, RED→GREEN — cost if wrong: none.
Task 3: complete (tests: go test ./internal/keygen -> ok, 11 tests incl. real ssh-keygen compat)
Task 4: Ruling: runner builds `ssh -- <alias>` and validates alias via config.ValidateAlias — an alias starting with '-' must never be parsed as an ssh option — cost if wrong: none.
Task 4: complete (tests: go test ./internal/ssh -> ok, 4 tests)
Task 5: Ruling: ui/styles is pure style constants, no unit tests of its own (rendering is asserted by view tests); help.go dropped, key hints live in the status bar — cost if wrong: none.
Task 5: Ruling: root model reloads ~/.ssh/config from disk before every mutation, so edits made outside elyms while it is open are never overwritten — cost if wrong: none.
Task 5: Ruling: added config.Rename and HostForm.SetErr/HostList.SetHeight — editing an alias must keep the block body (extras, comments), and paging must match the viewport — each added test-first, RED→GREEN — cost if wrong: none.
Task 5: Ruling: HostForm skips alias validation when the alias is unchanged — otherwise the existing `Host *` block could not be edited at all — found by test, RED→GREEN.
Task 5: complete (tests: go test ./internal/ui/... -> ok)
Task 6: complete (go build ok, elyms.exe 4.9 MB; go vet ./... clean; go test ./... all ok)
Final review: fresh general subagent, whole project. Verified against real ssh.exe. Findings entering the fix pass: C1 (# or " in a value breaks ssh for the whole file), C2 (alias allowlist), I1 (trailing comments), I2 (BOM), I3 (new host after Host * is overridden), I4 (duplicate aliases edit wrong block), I5 (ssh failure reason invisible / exit codes shown as errors), I6/I7/I8 (modal + inspector overflow), I9 (RSA freezes UI), I10 (.bak overwritten on each save).
Final: Ruling: I5 fixed by classifying exit codes (255 = connection failed with hint to run ssh in a shell; other codes = session ended), not by capturing ssh stderr — piping stderr changes OpenSSH's tty behavior and I cannot verify it on a real console — cost if wrong: user must re-run ssh by hand to read the reason.
Final: Ruling: I4 fixed by flagging duplicate aliases and refusing edit/delete/rename on them with a status message, not by tracking block identity — smaller, and duplicate Host lines are a config smell — cost if wrong: user fixes duplicates by hand.
Final: fixed C1, C2, I1-I10 (key manager and modals now fit the height; minHeight raised to 16; ssh.Describe wired in; edit/delete refused on duplicate aliases). go vet clean, go test ./... green, elyms.exe rebuilt (5.0 MB).
Final: minor (deferred): M2 private key is written before its ACL is restricted (window of milliseconds, file is removed if restriction fails).
Final: minor (deferred): M3 paste (non-KeyMsg) is not forwarded to focused inputs in the host form and generate form.
Final: minor (deferred): M4 first Up in the identity picker with nothing selected jumps to the first key.
Final: minor (deferred): M5 new hosts always write "Port 22" explicitly.
Final: minor (deferred): M9 elyms.exe is left in the repo root as the build output.
Ruling: no git repo and no commit steps in plan — track via this ledger, no git init — user did not ask for a repo — cost if wrong: none.
