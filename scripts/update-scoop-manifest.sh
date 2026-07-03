#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  update-scoop-manifest.sh --manifest PATH --tag TAG --checksums PATH [--repo OWNER/REPO]

Updates a Scoop manifest from release checksums.

Required:
  --manifest   Path to bucket/lazy-mcp-wrapper.json
  --tag        Release tag, for example v0.1.0
  --checksums  Path to SHA256SUMS generated from release archives

Optional:
  --repo       GitHub repository used in release URLs, default binlee/lazy-mcp-wrapper
USAGE
}

manifest_path=""
tag=""
checksums_path=""
repo="binlee/lazy-mcp-wrapper"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --manifest)
      manifest_path="${2:-}"
      shift 2
      ;;
    --tag)
      tag="${2:-}"
      shift 2
      ;;
    --checksums)
      checksums_path="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$manifest_path" || -z "$tag" || -z "$checksums_path" ]]; then
  usage >&2
  exit 2
fi

if [[ ! -f "$manifest_path" ]]; then
  echo "manifest not found: $manifest_path" >&2
  exit 1
fi

if [[ ! -f "$checksums_path" ]]; then
  echo "checksums not found: $checksums_path" >&2
  exit 1
fi

MANIFEST_PATH="$manifest_path" \
RELEASE_TAG="$tag" \
CHECKSUMS_PATH="$checksums_path" \
RELEASE_REPO="$repo" \
ruby <<'RUBY'
require "json"

manifest_path = ENV.fetch("MANIFEST_PATH")
tag = ENV.fetch("RELEASE_TAG")
checksums_path = ENV.fetch("CHECKSUMS_PATH")
repo = ENV.fetch("RELEASE_REPO")
version = tag.sub(/\Av/, "")
asset = "lazy-mcp-wrapper-windows-amd64.zip"

checksums = {}
File.readlines(checksums_path, chomp: true).each do |line|
  next if line.strip.empty?

  sha, file = line.split(/\s+/, 2)
  next if sha.nil? || file.nil?

  checksums[File.basename(file.strip)] = sha
end

abort("missing checksum for #{asset}") unless checksums.key?(asset)

manifest = JSON.parse(File.read(manifest_path))
manifest["version"] = version
manifest["homepage"] = "https://github.com/#{repo}"
manifest.fetch("architecture").fetch("64bit")["url"] = "https://github.com/#{repo}/releases/download/#{tag}/#{asset}"
manifest.fetch("architecture").fetch("64bit")["hash"] = checksums.fetch(asset)
manifest.fetch("autoupdate").fetch("architecture").fetch("64bit")["url"] =
  "https://github.com/#{repo}/releases/download/v$version/#{asset}"

File.write(manifest_path, JSON.pretty_generate(manifest, indent: "    ") + "\n")
RUBY

echo "updated $manifest_path to $tag"
