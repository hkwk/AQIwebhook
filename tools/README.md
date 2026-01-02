# Cross-compilation (Windows → Linux using Zig)

This document explains a small, **project-independent** way to cross-compile Rust targets (for example, `x86_64-unknown-linux-musl`) from Windows using Zig as a system linker/sysroot. It also covers alternative tools (`cargo-zigbuild`, `cross`) and troubleshooting tips.

## Summary ✅
- Two simple, reusable approaches:
  1. Create Zig wrapper scripts (linker/ar) in a folder on `PATH` and add a global Cargo config (`%USERPROFILE%\.cargo\config.toml`).
  2. Use helper tools like `cargo-zigbuild` or `cross` which require less manual setup.

---

## Prerequisites 🔧
- Install Zig and put it on your `PATH` (https://ziglang.org/download).
- Rust toolchain installed with `rustup`.
- Add the target you want to build:

```powershell
rustup target add x86_64-unknown-linux-musl
```

---

## Option A — Global Cargo config + Zig wrappers (recommended for control)

1. Create a central folder for wrappers (example): `C:\tools\zig-cross\bin` and add it to your Windows `PATH`.

2. Create the wrapper scripts. Example Windows CMD wrapper `zig-x86_64-linux-musl.cmd`:

```bat
@echo off
REM Acts as a linker for cargo
zig.exe cc -target x86_64-linux-musl -static %*
```

Example `zig-ar.cmd`:

```bat
@echo off
zig.exe ar %*
```

(If you use PowerShell, you can make `.ps1` wrapper versions; for WSL/Linux use a small shell script calling `zig cc -target ...`.)

3. Create a **global Cargo config** (applies to all projects) at `%USERPROFILE%\.cargo\config.toml` with the target section:

```toml
[target.x86_64-unknown-linux-musl]
linker = "zig-x86_64-linux-musl"
ar = "zig-ar"
rustflags = ["-C", "target-feature=+crt-static"]
```

- The `linker` and `ar` must be resolvable on `PATH` and match the wrapper filenames.

4. Build in any project:

```powershell
cargo build --release --target x86_64-unknown-linux-musl
```

---

## Option B — `cargo-zigbuild` (quick, minimal config)

Install and use:

```powershell
cargo install cargo-zigbuild
rustup target add x86_64-unknown-linux-musl
cargo zigbuild --target x86_64-unknown-linux-musl --release
```

This uses Zig automatically and avoids per-project or global config edits.

---

## Option C — `cross` (uses Docker/qemu, heavier but reproducible)

```powershell
cargo install cross
cross build --target x86_64-unknown-linux-musl --release
```

---

## Troubleshooting & Tips ⚠️
- "linker not found": ensure wrapper name and location are on `PATH` and match the `linker` setting.
- Static linking: when targeting `*-musl`, you may need `-C target-feature=+crt-static` (`rustflags`) to avoid dynamic glibc.
- If Rust crates require system libraries, ensure they are available in the Zig sysroot or avoid those crates.
- Add other targets by creating additional wrapper scripts and Cargo `target.*` sections.

---

## Next steps / Useful additions 💡
- I can add example wrapper scripts directly into this repo under `tools/` (Windows `.cmd` + PowerShell + Unix sh) and a sample global `config.toml` you can copy to `%USERPROFILE%\.cargo\config.toml`.

---

If you'd like, I can add the sample scripts and a copyable `config.toml` to this repo and show commands to add them to your `PATH` on Windows.
