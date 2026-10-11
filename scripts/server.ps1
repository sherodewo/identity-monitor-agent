param(
    [ValidateSet('run', 'status', 'stop')]
    [string]$Action = 'run',
    [int]$Port = 8081
)

$ErrorActionPreference = 'Stop'
$projectDirectory = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$binaryPath = Join-Path $projectDirectory 'bin\identity-monitor-agent.exe'
$subjectsPath = Join-Path $projectDirectory 'configs\subjects.yaml'

if (-not (Test-Path -LiteralPath $binaryPath)) {
    throw "Binary belum tersedia: $binaryPath. Jalankan .\scripts\dev.ps1 build"
}

function Get-WorkspaceServer {
    $connections = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    foreach ($connection in $connections) {
        $process = Get-Process -Id $connection.OwningProcess -ErrorAction SilentlyContinue
        if ($process -and $process.Path -eq $binaryPath) {
            return $process
        }
    }
    return $null
}

switch ($Action) {
    'status' {
        $process = Get-WorkspaceServer
        if ($process) {
            Write-Output "RUNNING - PID $($process.Id) - http://127.0.0.1:$Port"
        } else {
            Write-Output "STOPPED - tidak ada server proyek pada port $Port"
        }
    }
    'stop' {
        $process = Get-WorkspaceServer
        if (-not $process) {
            Write-Output "Server proyek tidak sedang berjalan pada port $Port"
            break
        }
        Stop-Process -Id $process.Id
        $process.WaitForExit()
        Write-Output "STOPPED - PID $($process.Id)"
    }
    'run' {
        if (Get-WorkspaceServer) {
            throw "Server sudah berjalan. Cek dengan: .\scripts\server.ps1 status"
        }
        Set-Location $projectDirectory
        Write-Output "RUNNING in foreground - http://127.0.0.1:$Port"
        Write-Output "Tekan Ctrl+C pada terminal ini untuk menghentikan server."
        & $binaryPath serve --subjects=$subjectsPath --address="127.0.0.1:$Port"
        exit $LASTEXITCODE
    }
}
