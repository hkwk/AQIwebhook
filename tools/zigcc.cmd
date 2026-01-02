@echo off
REM Wrapper for cargo/rustc: use Zig as the C linker/driver
zig cc %*
