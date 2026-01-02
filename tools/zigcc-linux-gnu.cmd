@echo off
setlocal EnableExtensions EnableDelayedExpansion
REM Zig C compiler wrapper for Linux x86_64 gnu
REM Some build scripts pass Rust-style triples like --target=x86_64-unknown-linux-gnu.
REM Zig can't parse that; strip/ignore it and force the Zig target instead.

set "filtered="
:loop
if "%~1"=="" goto done

set "arg=%~1"

REM cc-rs probes MSVC via `-?` / `/?`. Force failure so cc-rs doesn't treat us as cl.exe.
if /I "!arg!"=="-?" exit /b 1
if /I "!arg!"=="/?" exit /b 1

REM Drop end-of-options marker if present
if /I "!arg!"=="--" (
	shift
	goto loop
)

REM Drop any --target=... (we force Zig target ourselves)
if /I "!arg:~0,9!"=="--target=" (
	shift
	goto loop
)

REM Drop problematic Rust-style target args
if /I "!arg!"=="--target=x86_64-unknown-linux-gnu" (
	shift
	goto loop
)
if /I "!arg!"=="--target" (
	REM If next is a Rust-style triple, drop both
	if /I "%~2"=="x86_64-unknown-linux-gnu" (
		shift
		shift
		goto loop
	)
)

REM Keep everything else
set "filtered=!filtered! !arg!"
shift
goto loop

:done
zig cc -target x86_64-linux-gnu !filtered!
endlocal
