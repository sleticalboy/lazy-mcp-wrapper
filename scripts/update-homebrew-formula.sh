#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  update-homebrew-formula.sh --formula PATH --tag TAG --checksums PATH [--repo OWNER/REPO]

Updates a Homebrew formula from release checksums.

Required:
  --formula    Path to Formula/lazy-mcp-wrapper.rb
  --tag        Release tag, for example v0.1.0
  --checksums  Path to SHA256SUMS generated from release archives

Optional:
  --repo       GitHub repository used in release URLs, default sleticalboy/lazy-mcp-wrapper
USAGE
}

formula_path=""
tag=""
checksums_path=""
repo="sleticalboy/lazy-mcp-wrapper"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --formula)
      formula_path="${2:-}"
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

if [[ -z "$formula_path" || -z "$tag" || -z "$checksums_path" ]]; then
  usage >&2
  exit 2
fi

if [[ ! -f "$formula_path" ]]; then
  echo "formula not found: $formula_path" >&2
  exit 1
fi

if [[ ! -f "$checksums_path" ]]; then
  echo "checksums not found: $checksums_path" >&2
  exit 1
fi

FORMULA_PATH="$formula_path" \
RELEASE_TAG="$tag" \
CHECKSUMS_PATH="$checksums_path" \
RELEASE_REPO="$repo" \
ruby <<'RUBY'
formula_path = ENV.fetch("FORMULA_PATH")
tag = ENV.fetch("RELEASE_TAG")
checksums_path = ENV.fetch("CHECKSUMS_PATH")
repo = ENV.fetch("RELEASE_REPO")
version = tag.sub(/\Av/, "")

assets = [
  "lazy-mcp-wrapper-darwin-arm64.tar.gz",
  "lazy-mcp-wrapper-darwin-amd64.tar.gz",
  "lazy-mcp-wrapper-linux-amd64.tar.gz"
]

checksums = {}
File.readlines(checksums_path, chomp: true).each do |line|
  next if line.strip.empty?

  sha, file = line.split(/\s+/, 2)
  next if sha.nil? || file.nil?

  name = File.basename(file.strip)
  checksums[name] = sha
end

missing = assets.reject { |asset| checksums.key?(asset) }
unless missing.empty?
  abort("missing checksums for: #{missing.join(", ")}")
end

lines = File.readlines(formula_path, chomp: true)
version_updated = false
lines.map! do |line|
  if line.match?(/^\s*version\s+"[^"]+"/)
    version_updated = true
    line.sub(/version\s+"[^"]+"/, %(version "#{version}"))
  else
    line
  end
end
abort("formula version line not found") unless version_updated

assets.each do |asset|
  sha = checksums.fetch(asset)
  url_index = lines.find_index { |line| line.include?("github.com/#{repo}/releases/download/") && line.include?("/#{asset}\"") }
  abort("formula url line not found for #{asset}") if url_index.nil?

  indent = lines[url_index][/^\s*/] || ""
  lines[url_index] = %(#{indent}url "https://github.com/#{repo}/releases/download/#{tag}/#{asset}")

  sha_index = (url_index + 1...lines.length).find do |idx|
    lines[idx].match?(/^\s*sha256\s+"[^"]+"/)
  end
  abort("formula sha256 line not found for #{asset}") if sha_index.nil?

  lines[sha_index] = lines[sha_index].sub(/sha256\s+"[^"]+"/, %(sha256 "#{sha}"))
end

File.write(formula_path, lines.join("\n") + "\n")
RUBY

echo "updated $formula_path to $tag"
