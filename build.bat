@echo off
setlocal enabledelayedexpansion

echo ========================================
echo   NoteVault Build Only
echo ========================================
echo.

REM --- Step 1: Setup PATH ---
echo [1/3] Setting up environment...
set "GIT_USR=C:\Users\feng\scoop\apps\git\current\usr\bin"
set "NODE_DIR=C:\Users\feng\scoop\apps\nodejs-lts\current"
set "GO_BIN=%USERPROFILE%\go\bin"

if exist "%GIT_USR%" set "PATH=%GIT_USR%;%PATH%"
if exist "%NODE_DIR%" set "PATH=%NODE_DIR%;%NODE_DIR%\bin;%PATH%"
if exist "%GO_BIN%" set "PATH=%GO_BIN%;%PATH%"
set "PACKAGE_MANAGER=pnpm"
echo   Done.
echo.

REM --- Step 2: Verify tools ---
echo [2/3] Checking tools...
call node --version
call pnpm --version
call go version
call wails3 version
echo.

REM --- Step 3: Build ---
echo [3/3] Building NoteVault...
echo   (This may take 1-2 minutes, please wait...)
echo.
cd /d "%~dp0"
call wails3 build
set BUILD_ERROR=%errorlevel%
echo.

if %BUILD_ERROR% neq 0 (
    echo ========================================
    echo   [BUILD FAILED] Error code: %BUILD_ERROR%
    echo   Please check the output above.
    echo ========================================
    echo.
    pause
    exit /b %BUILD_ERROR%
)

echo ========================================
echo   Build successful!
echo   Output: %~dp0bin\notevault.exe
echo ========================================
echo.
pause
