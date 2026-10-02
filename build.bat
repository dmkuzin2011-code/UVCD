@echo off
rem
rem build.bat - build the Go device scanner as a native Windows executable.
rem
rem Usage:
rem   build.bat
rem
rem Output:
rem   bin\scan-devices.exe    (windows/amd64)
rem
setlocal
cd /d "%~dp0"

if not exist bin mkdir bin

echo ==^> Building Windows binary: bin\scan-devices.exe
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -X main.buildOS=windows -X main.buildArch=amd64" -o bin\scan-devices.exe .\scanner
if errorlevel 1 goto :err

echo ==^> Done
dir bin
exit /b 0

:err
echo Build failed.
exit /b 1