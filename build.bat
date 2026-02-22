@echo off
:: build.bat -- build UBR for all target platforms
:: Usage: build.bat [VERSION]
setlocal enabledelayedexpansion

:: ── version ──────────────────────────────────────────────────────────────────
for /f "usebackq tokens=* delims=" %%v in ("VERSION") do (
    set BASE_VERSION=%%v
    goto :got_base
)
:got_base
:: Strip any trailing whitespace/CR from VERSION
set BASE_VERSION=%BASE_VERSION: =%

for /f "tokens=* delims=" %%h in ('git rev-parse --short HEAD 2^>nul') do set GIT_HASH=%%h
if not defined GIT_HASH set GIT_HASH=nogit

set VERSION=%BASE_VERSION%-%GIT_HASH%

:: Allow caller to pass an explicit version as first argument
if not "%~1"=="" set VERSION=%~1

set LDFLAGS=-s -w -X main.version=%VERSION%
set OUTDIR=dist

echo Building UBR %VERSION%
echo Output: %OUTDIR%\
echo.

if not exist %OUTDIR% mkdir %OUTDIR%

:: ── build helper ─────────────────────────────────────────────────────────────
:: Note: raw socket (rebroadcast) and AF_PACKET (monitor) are Linux-only.
::       Windows/macOS builds compile but those features are gracefully disabled.

set GOARM=

:: linux/amd64
set GOOS=linux& set GOARCH=amd64
echo   linux/amd64          ->  %OUTDIR%\ubr-linux-amd64
go build -trimpath -ldflags="%LDFLAGS%" -o %OUTDIR%\ubr-%GOOS%-%GOARCH% .\cmd\ubr
if errorlevel 1 goto :error

:: linux/arm64
set GOOS=linux& set GOARCH=arm64
echo   linux/arm64          ->  %OUTDIR%\ubr-linux-arm64
go build -trimpath -ldflags="%LDFLAGS%" -o %OUTDIR%\ubr-%GOOS%-%GOARCH% .\cmd\ubr
if errorlevel 1 goto :error

:: linux/arm (ARMv7, covers Raspberry Pi 3 and most 32-bit ARM boards)
set GOOS=linux& set GOARCH=arm& set GOARM=7
echo   linux/arm            ->  %OUTDIR%\ubr-linux-arm
go build -trimpath -ldflags="%LDFLAGS%" -o %OUTDIR%\ubr-%GOOS%-%GOARCH% .\cmd\ubr
if errorlevel 1 goto :error
set GOARM=

:: windows/amd64
set GOOS=windows& set GOARCH=amd64
echo   windows/amd64        ->  %OUTDIR%\ubr-windows-amd64.exe
go build -trimpath -ldflags="%LDFLAGS%" -o %OUTDIR%\ubr-%GOOS%-%GOARCH%.exe .\cmd\ubr
if errorlevel 1 goto :error

:: darwin/amd64
set GOOS=darwin& set GOARCH=amd64
echo   darwin/amd64         ->  %OUTDIR%\ubr-darwin-amd64
go build -trimpath -ldflags="%LDFLAGS%" -o %OUTDIR%\ubr-%GOOS%-%GOARCH% .\cmd\ubr
if errorlevel 1 goto :error

:: darwin/arm64 (Apple Silicon)
set GOOS=darwin& set GOARCH=arm64
echo   darwin/arm64         ->  %OUTDIR%\ubr-darwin-arm64
go build -trimpath -ldflags="%LDFLAGS%" -o %OUTDIR%\ubr-%GOOS%-%GOARCH% .\cmd\ubr
if errorlevel 1 goto :error

:: ── checksums ────────────────────────────────────────────────────────────────
echo.
echo Generating checksums...
:: Use certutil (built-in) to produce SHA256 hashes, format similar to sha256sum
(for %%f in (%OUTDIR%\ubr-*) do (
    for /f "skip=1 tokens=* delims=" %%h in ('certutil -hashfile "%%f" SHA256') do (
        echo %%h  %%~nxf
        goto :next_%%~nxf
    )
    :next_%%~nxf
)) > %OUTDIR%\SHA256SUMS.txt
echo   %OUTDIR%\SHA256SUMS.txt

echo.
echo Done.
dir /b %OUTDIR%\
endlocal
goto :eof

:error
echo.
echo Build failed.
endlocal
exit /b 1
