@echo off
setlocal enabledelayedexpansion

echo ========================================
echo   NoteVault Build and Run
echo ========================================
echo.

REM --- Step 1: Setup PATH ---
echo [1/4] Setting up environment...
set "GIT_USR=C:\Users\feng\scoop\apps\git\current\usr\bin"
set "NODE_DIR=C:\Users\feng\scoop\apps\nodejs-lts\current"
set "GO_BIN=%USERPROFILE%\go\bin"

if exist "%GIT_USR%" (
    set "PATH=%GIT_USR%;%PATH%"
    echo   Git tools: OK
) else (
    echo   [WARN] Git usr bin not found: %GIT_USR%
)

if exist "%NODE_DIR%" (
    set "PATH=%NODE_DIR%;%NODE_DIR%\bin;%PATH%"
    echo   Node.js: OK
) else (
    echo   [ERROR] Node.js not found: %NODE_DIR%
    pause
    exit /b 1
)

if exist "%GO_BIN%" (
    set "PATH=%GO_BIN%;%PATH%"
    echo   Go bin: OK
) else (
    echo   [WARN] Go bin not found: %GO_BIN%
)

set "PACKAGE_MANAGER=pnpm"
echo.

REM --- Step 2: Verify tools ---
echo [2/4] Checking tools...
call node --version
if errorlevel 1 (
    echo [ERROR] node not found
    pause
    exit /b 1
)
call pnpm --version
if errorlevel 1 (
    echo [ERROR] pnpm not found
    pause
    exit /b 1
)
call go version
if errorlevel 1 (
    echo [ERROR] go not found
    pause
    exit /b 1
)
call wails3 version
if errorlevel 1 (
    echo [ERROR] wails3 not found
    pause
    exit /b 1
)
echo.

REM --- Step 3: Build ---
echo [3/4] Building NoteVault...
echo   (This may take 1-2 minutes, please wait...)
echo.
cd /d "%~dp0"
call wails3 build
set BUILD_ERROR=%errorlevel%
echo.

if %BUILD_ERROR% neq 0 (
    echo ========================================
    echo   [BUILD FAILED] Error code: %BUILD_ERROR%
    echo   Please check the output above for details.
    echo ========================================
    echo.
    pause
    exit /b %BUILD_ERROR%
)

echo   Build successful!
echo.

REM --- Step 4: Run ---
echo [4/4] Launching NoteVault...
if exist "%~dp0bin\notevault.exe" (
    start "" "%~dp0bin\notevault.exe"
    echo   App launched!
) else (
    echo   [ERROR] exe not found: %~dp0bin\notevault.exe
    pause
    exit /b 1
)

echo.
echo ========================================
echo   Done! Window will close in 5 seconds.
echo ========================================
timeout /t 5 >nul
