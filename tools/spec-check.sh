#!/usr/bin/env sh
# Check an engine's vendored copy of the definition.
#
# Intact: every vendored file matches its digest in the manifest vendored
# beside it, the manifest's own digest matches the files it lists, and the
# corpus contains no file that the manifest does not list. A hand-edited,
# half-copied or extended copy fails, offline. Only the overlay of the
# language that overlay.json declares, and only that language's fixtures,
# are compared.
#
# Current: the manifest at the pinned ref is fetched and the digests are
# compared. A difference is reported by default, because an engine may lag
# a change while it catches up.
#
# Pass --strict to make a difference fail. CI uses it on a change that
# touches the vendored copy: falling behind is fine, and committing a copy
# that matches nothing anyone else has is not. Under --strict, an upstream
# that cannot be read fails too, because the comparison did not happen.
#
# Exit status: 0 when the checks pass, 1 when the copy is not intact or,
# under --strict, differs from upstream, and 3 when --strict cannot read
# upstream.
#
# SPEC_REF names the ref to compare against, main by default. SPEC_RAW
# replaces the whole base URL, such as a file:// URL of a checkout, so a
# test can run the comparison offline.
set -eu

STRICT=0
DEST=""
for arg in "$@"; do
    case "$arg" in
        --strict) STRICT=1 ;;
        *) if [ -z "$DEST" ]; then DEST=$arg; fi ;;
    esac
done
[ -n "$DEST" ] || { echo "usage: spec-check.sh <vendored directory> [--strict]"; exit 2; }
REF=${SPEC_REF:-main}
RAW=${SPEC_RAW:-https://raw.githubusercontent.com/dokimasia/mutate-spec/$REF}

python3 - "$DEST" "$(dirname "$0")" <<'PY_INNER'
import hashlib, json, pathlib, sys

dest = pathlib.Path(sys.argv[1])
tools = pathlib.Path(sys.argv[2])
manifest = json.loads((dest / "manifest.json").read_text())
files = manifest["files"]
language = json.loads((dest / "overlay.json").read_text())["language"]

def local(name):
    parts = name.split("/")
    if parts[:2] == ["spec", "corpus"]:
        if len(parts) > 3 and (parts[3:] == ["case.json"] or parts[3] == language):
            return dest.joinpath("corpus", *parts[2:])
        return None
    if parts[:2] == ["spec", "overlays"]:
        return dest / "overlay.json" if name == f"spec/overlays/{language}.json" else None
    if parts[0] == "tools":
        return tools / parts[-1]
    return dest / parts[-1]

problems = []
if f"spec/overlays/{language}.json" not in files:
    problems.append(f"overlay.json declares {language!r}, and the manifest lists no overlay for it")

joined = "".join(f"{name} {sha}\n" for name, sha in sorted(files.items()))
if "sha256:" + hashlib.sha256(joined.encode()).hexdigest() != manifest["digest"]:
    problems.append("the manifest's digest is not the digest of the files it lists")

checked, wrong, missing, listed = 0, [], [], set()
for name, want in sorted(files.items()):
    path = local(name)
    if path is None:
        continue
    listed.add(path)
    if not path.exists():
        missing.append(name)
        continue
    got = "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()
    checked += 1
    if got != want:
        wrong.append(name)

corpus = dest / "corpus"
extra = sorted(str(p.relative_to(dest)) for p in corpus.rglob("*") if p.is_file() and p not in listed) if corpus.exists() else []

if missing:
    problems.append("vendored copy is missing " + ", ".join(missing))
if wrong:
    problems.append("these do not match the manifest beside them: " + ", ".join(wrong))
if extra:
    problems.append("the manifest does not list " + ", ".join(extra))
for problem in problems:
    print("spec: " + problem)
if problems:
    raise SystemExit(1)

print(f"spec: {checked} files intact at {manifest['version']} {manifest['digest'][:19]}")
PY_INNER

mine=$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["digest"])' "$DEST/manifest.json")
theirs=$(curl -fsSL --max-time 20 "$RAW/spec/manifest.json" 2>/dev/null \
    | python3 -c 'import json, sys; print(json.load(sys.stdin)["digest"])' 2>/dev/null) || theirs=""

if [ -z "$theirs" ]; then
    echo "spec: could not reach $REF, so it was not compared"
    if [ "$STRICT" = "1" ]; then
        echo "spec: this change touches the vendored copy, so it has to be compared"
        exit 3
    fi
    exit 0
fi

if [ "$mine" = "$theirs" ]; then
    echo "spec: matches $REF"
    exit 0
fi

# Behind, ahead, or taken from a checkout nobody pushed. One fetch cannot
# tell those apart, so it prints both digests.
echo "spec: differs from $REF"
echo "  vendored $mine"
echo "  upstream $theirs"
echo "  run: ./tools/spec-sync.sh $DEST <language>"

if [ "$STRICT" = "1" ]; then
    echo "spec: this change touches the vendored copy, so it has to match"
    exit 1
fi
