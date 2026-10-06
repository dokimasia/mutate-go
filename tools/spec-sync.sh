#!/usr/bin/env sh
# Refresh an engine's vendored copy of the definition.
#
# The script fetches a pinned ref, so the same command copies the same
# files on a laptop and on a runner. It copies VERSION, the catalogue, the
# protocol, the record schema, the manifest, the language's overlay as
# overlay.json, and of each corpus case its case.json and the language's
# fixture. The corpus is replaced as a whole, so a file that the definition
# dropped does not remain.
#
# SPEC_LOCAL takes a sibling checkout instead, to try a change before it is
# pushed. The script prints a warning, because no other machine can
# reproduce the copy it leaves.
set -eu

USAGE="usage: spec-sync.sh <destination directory> <overlay language>"
DEST=${1:?$USAGE}
OVERLAY=${2:?$USAGE}
REF=${SPEC_REF:-main}
RAW="https://raw.githubusercontent.com/dokimasia/mutate-spec/$REF"

FILES="VERSION spec/catalogue.json spec/protocol.json spec/record.schema.json spec/manifest.json"

fetch() {
    # $1 repository-relative path, $2 the destination file
    mkdir -p "$(dirname "$2")"
    if [ -n "${SPEC_LOCAL:-}" ]; then
        cp "$SPEC_LOCAL/$1" "$2"
    else
        curl -fsSL "$RAW/$1" -o "$2"
    fi
}

if [ -n "${SPEC_LOCAL:-}" ]; then
    echo "spec: taking $SPEC_LOCAL, not $REF"
    echo "spec: the copy this leaves is reproducible nowhere else; do not commit it"
fi

for f in $FILES; do
    fetch "$f" "$DEST/$(basename "$f")"
done
fetch "spec/overlays/$OVERLAY.json" "$DEST/overlay.json"

# The manifest lists the corpus, so the copy contains exactly the listed
# files of the language's fixtures.
names=$(python3 - "$DEST/manifest.json" "$OVERLAY" <<'PY'
import json, sys
files = json.load(open(sys.argv[1]))["files"]
language = sys.argv[2]
for name in sorted(files):
    parts = name.split("/")
    if parts[:2] != ["spec", "corpus"] or len(parts) < 4:
        continue
    if parts[3:] == ["case.json"] or parts[3] == language:
        print(name)
PY
)

rm -rf "$DEST/corpus"
for name in $names; do
    fetch "$name" "$DEST/corpus/${name#spec/corpus/}"
done

# The scripts are vendored with the definition, so every engine runs the
# same copy. Each is renamed into place, so the copy running now is never
# written through.
TOOLS=$(dirname "$0")
for script in spec-sync.sh spec-check.sh; do
    fetch "tools/$script" "$TOOLS/$script.new"
    chmod +x "$TOOLS/$script.new"
    mv "$TOOLS/$script.new" "$TOOLS/$script"
done

[ -n "${SPEC_LOCAL:-}" ] || echo "spec: fetched $REF"
exec "$(dirname "$0")/spec-check.sh" "$DEST"
