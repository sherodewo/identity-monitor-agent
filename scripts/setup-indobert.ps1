$ErrorActionPreference = "Stop"

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$modelDirectory = Join-Path $projectRoot "models\indobert-sentiment"
$runtimeDirectory = Join-Path $projectRoot "runtime\onnxruntime"
$downloadDirectory = Join-Path $projectRoot ".tools\indobert-download"

New-Item -ItemType Directory -Force $modelDirectory, $runtimeDirectory, $downloadDirectory | Out-Null

$modelRevision = "d716509a8f4bdcb214b8680059a55754c15c6105"
$modelBase = "https://huggingface.co/mshbljrngoding/indonesia-bert-sentiment-classification-quantize/resolve/$modelRevision"
$modelDownload = Join-Path $downloadDirectory "model.onnx"
$vocabDownload = Join-Path $downloadDirectory "vocab.txt"
$runtimeZip = Join-Path $downloadDirectory "onnxruntime-win-x64-1.29.0.zip"
$runtimeExtract = Join-Path $downloadDirectory "onnxruntime-win-x64-1.29.0"

function Get-Asset([string]$Uri, [string]$Destination) {
    if (-not (Test-Path -LiteralPath $Destination)) {
        Write-Host "Downloading $([IO.Path]::GetFileName($Destination))..."
        Invoke-WebRequest -Uri $Uri -OutFile $Destination
    }
}

Get-Asset "$modelBase/indobert-quant-dynamic.onnx" $modelDownload
Get-Asset "$modelBase/vocab.txt" $vocabDownload
Get-Asset "https://github.com/microsoft/onnxruntime/releases/download/v1.29.0/onnxruntime-win-x64-1.29.0.zip" $runtimeZip

if ((Get-Item -LiteralPath $modelDownload).Length -lt 119MB) {
    throw "The downloaded IndoBERT model is incomplete. Delete $modelDownload and retry."
}
if ((Get-Item -LiteralPath $runtimeZip).Length -lt 70MB) {
    throw "The downloaded ONNX Runtime archive is incomplete. Delete $runtimeZip and retry."
}

if (-not (Test-Path -LiteralPath $runtimeExtract)) {
    Expand-Archive -LiteralPath $runtimeZip -DestinationPath $downloadDirectory
}

Copy-Item -LiteralPath $modelDownload -Destination (Join-Path $modelDirectory "model.onnx") -Force
Copy-Item -LiteralPath $vocabDownload -Destination (Join-Path $modelDirectory "vocab.txt") -Force
Copy-Item -LiteralPath (Join-Path $runtimeExtract "lib\onnxruntime.dll") -Destination (Join-Path $runtimeDirectory "onnxruntime.dll") -Force

Write-Host "IndoBERT is ready."
Write-Host "Model:  models\indobert-sentiment\model.onnx"
Write-Host "Runtime: runtime\onnxruntime\onnxruntime.dll"
