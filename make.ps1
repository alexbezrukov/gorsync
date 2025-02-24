# Parse command-line arguments
param (
    [string]$Task = "Build-All"
)

# Project settings
$APP_NAME = "gorsync"
$BUILD_DIR = "build"
$SOURCE_DIR = "cmd"

# Ensure build directory exists
if (!(Test-Path $BUILD_DIR)) {
    New-Item -ItemType Directory -Path $BUILD_DIR | Out-Null
}

# Default Go settings
$GO = "go"

Function Build {
    Write-Host "Building $APP_NAME..."
    & $GO build -o "$BUILD_DIR/$APP_NAME" "$SOURCE_DIR/main.go"
    Write-Host "Build complete. Binary available at $BUILD_DIR/$APP_NAME"
}

Function Run-Server {
    Write-Host "Starting server..."
    & "$BUILD_DIR/$APP_NAME" server
}

Function Run-Client {
    Write-Host "Starting client..."
    & "$BUILD_DIR/$APP_NAME" client --source="/path/to/source" --dest="127.0.0.1:8080"
}

Function Build-Linux {
    Write-Host "Building Linux binary..."
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    & $GO build -o "$BUILD_DIR/$APP_NAME-linux" "$SOURCE_DIR/main.go"
}

Function Build-Windows {
    Write-Host "Building Windows binary..."
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    & $GO build -o "$BUILD_DIR/$APP_NAME.exe" "$SOURCE_DIR/main.go"
}

Function Build-Macos {
    Write-Host "Building macOS binary..."
    $env:GOOS = "darwin"
    $env:GOARCH = "amd64"
    & $GO build -o "$BUILD_DIR/$APP_NAME-darwin" "$SOURCE_DIR/main.go"
}

Function Build-All {
    Build-Linux
    Build-Windows
    Build-Macos
    Write-Host "All platform builds complete. Binaries available in $BUILD_DIR/"
}

Function Test {
    Write-Host "Running tests..."
    & $GO test ./...
}

Function Clean {
    Write-Host "Cleaning up..."
    Remove-Item -Recurse -Force $BUILD_DIR -ErrorAction SilentlyContinue
    Write-Host "Clean complete."
}

switch ($Task) {
    "build" { Build }
    "run-server" { Run-Server }
    "run-client" { Run-Client }
    "build-linux" { Build-Linux }
    "build-windows" { Build-Windows }
    "build-macos" { Build-Macos }
    "build-all" { Build-All }
    "test" { Test }
    "clean" { Clean }
    default { Write-Host "Unknown task: $Task" }
}
