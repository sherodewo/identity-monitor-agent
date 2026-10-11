param([ValidateSet('build','test','test-race','run','demo','migrate','report','lint')][string]$Task='demo')
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
$goExe=if(Test-Path '.tools/go/bin/go.exe'){(Resolve-Path '.tools/go/bin/go.exe').Path}else{'go'}
switch($Task){
 'build' { & $goExe build -ldflags '-linkmode=internal' -o bin/identity-monitor-agent.exe ./cmd/collector }
 'test' { & $goExe test ./... }
 'test-race' { $env:CGO_ENABLED='1'; & $goExe test -race ./... }
 'lint' { if(Test-Path '.tools/bin/golangci-lint.exe'){ & ./.tools/bin/golangci-lint.exe run }else{ & golangci-lint run } }
 'run' { & $goExe run ./cmd/collector serve }
 default { & $goExe run ./cmd/collector $Task }
}
exit $LASTEXITCODE
