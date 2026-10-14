#!/bin/sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
model_dir="$project_root/models/indobert-sentiment"
runtime_dir="$project_root/runtime/onnxruntime"
download_dir="$project_root/.tools/indobert-download"

model_revision="d716509a8f4bdcb214b8680059a55754c15c6105"
model_base="https://huggingface.co/mshbljrngoding/indonesia-bert-sentiment-classification-quantize/resolve/$model_revision"
runtime_version="1.29.0"

case "$(uname -m)" in
  arm64) runtime_platform="osx-arm64" ;;
  x86_64) runtime_platform="osx-x86_64" ;;
  *) echo "Unsupported macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$model_dir" "$runtime_dir" "$download_dir"
model_download="$download_dir/model.onnx"
vocab_download="$download_dir/vocab.txt"
runtime_archive="$download_dir/onnxruntime-$runtime_platform-$runtime_version.tgz"
runtime_extract="$download_dir/onnxruntime-$runtime_platform-$runtime_version"

download() {
  url=$1
  destination=$2
  if [ ! -f "$destination" ]; then
    echo "Downloading $(basename "$destination")..."
    curl -fL --retry 3 --retry-delay 2 "$url" -o "$destination"
  fi
}

download "$model_base/indobert-quant-dynamic.onnx" "$model_download"
download "$model_base/vocab.txt" "$vocab_download"
download "https://github.com/microsoft/onnxruntime/releases/download/v$runtime_version/onnxruntime-$runtime_platform-$runtime_version.tgz" "$runtime_archive"

model_size=$(wc -c < "$model_download" | tr -d ' ')
runtime_size=$(wc -c < "$runtime_archive" | tr -d ' ')
if [ "$model_size" -lt 120000000 ]; then
  echo "Downloaded IndoBERT model is incomplete; remove $model_download and retry." >&2
  exit 1
fi
if [ "$runtime_size" -lt 5000000 ]; then
  echo "Downloaded ONNX Runtime archive is incomplete; remove $runtime_archive and retry." >&2
  exit 1
fi

if [ ! -d "$runtime_extract" ]; then
  tar -xzf "$runtime_archive" -C "$download_dir"
fi

cp "$model_download" "$model_dir/model.onnx"
cp "$vocab_download" "$model_dir/vocab.txt"
cp "$runtime_extract/lib/libonnxruntime.dylib" "$runtime_dir/libonnxruntime.dylib"
chmod 600 "$model_dir/model.onnx" "$model_dir/vocab.txt" "$runtime_dir/libonnxruntime.dylib"

echo "IndoBERT is ready."
echo "Model:   models/indobert-sentiment/model.onnx"
echo "Runtime: runtime/onnxruntime/libonnxruntime.dylib"
