#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3126}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
DIR=$(e2e_native_tempdir)
BIN_DIR=$(e2e_native_tempdir)
e2e_cleanup_install
e2e_cleanup_add "$DIR" "$BIN_DIR"

cd "$DIR"

# A lone Excalidraw scene, which preview mode opens as a canvas page
# (`crit arch.excalidraw`). Two shapes with labels, an arrow with a label and
# one without, and free text. The "query" label's saved x/y are stale on
# purpose: the renderer places an arrow's label from the arrow, as
# Excalidraw does.
cat > arch.excalidraw << 'EXCALIDRAWFILE'
{
  "type": "excalidraw",
  "version": 2,
  "source": "https://excalidraw.com",
  "elements": [
    {
      "id": "auth", "type": "rectangle", "x": 0, "y": 0, "width": 200, "height": 80,
      "strokeColor": "#1e1e1e", "backgroundColor": "#a5d8ff", "fillStyle": "solid",
      "strokeWidth": 2, "roughness": 1, "opacity": 100, "seed": 11, "roundness": { "type": 3 },
      "boundElements": [{ "type": "text", "id": "auth-label" }, { "type": "arrow", "id": "query" }, { "type": "arrow", "id": "reply" }]
    },
    {
      "id": "auth-label", "type": "text", "x": 45, "y": 27.5, "width": 110, "height": 25,
      "text": "Auth service", "originalText": "Auth service", "fontSize": 20, "fontFamily": 5,
      "textAlign": "center", "verticalAlign": "middle", "containerId": "auth", "strokeColor": "#1e1e1e", "seed": 12
    },
    {
      "id": "db", "type": "ellipse", "x": 400, "y": -10, "width": 180, "height": 100,
      "strokeColor": "#1e1e1e", "backgroundColor": "#b2f2bb", "fillStyle": "solid",
      "strokeWidth": 2, "roughness": 1, "opacity": 100, "seed": 21,
      "boundElements": [{ "type": "text", "id": "db-label" }, { "type": "arrow", "id": "query" }, { "type": "arrow", "id": "reply" }]
    },
    {
      "id": "db-label", "type": "text", "x": 445, "y": 27.5, "width": 90, "height": 25,
      "text": "Orders DB", "originalText": "Orders DB", "fontSize": 20, "fontFamily": 5,
      "textAlign": "center", "verticalAlign": "middle", "containerId": "db", "strokeColor": "#1e1e1e", "seed": 22
    },
    {
      "id": "query", "type": "arrow", "x": 200, "y": 25, "width": 200, "height": 0,
      "points": [[0, 0], [200, 0]], "strokeColor": "#1e1e1e", "strokeWidth": 2, "roughness": 1, "seed": 31,
      "startBinding": { "elementId": "auth" }, "endBinding": { "elementId": "db" }, "endArrowhead": "arrow",
      "boundElements": [{ "type": "text", "id": "query-label" }]
    },
    {
      "id": "query-label", "type": "text", "x": 0, "y": 0, "width": 50, "height": 25,
      "text": "query", "originalText": "query", "fontSize": 20, "fontFamily": 5,
      "textAlign": "center", "verticalAlign": "middle", "containerId": "query", "strokeColor": "#1e1e1e", "seed": 32
    },
    {
      "id": "reply", "type": "arrow", "x": 400, "y": 60, "width": 200, "height": 0,
      "points": [[0, 0], [-200, 0]], "strokeColor": "#1e1e1e", "strokeWidth": 2, "roughness": 1, "seed": 41,
      "startBinding": { "elementId": "db" }, "endBinding": { "elementId": "auth" }, "endArrowhead": "arrow"
    },
    {
      "id": "note", "type": "text", "x": 0, "y": 140, "width": 300, "height": 25,
      "text": "Pins name the element they are on", "originalText": "Pins name the element they are on",
      "fontSize": 20, "fontFamily": 5, "textAlign": "left", "strokeColor": "#1e1e1e", "seed": 51
    }
  ],
  "appState": { "viewBackgroundColor": "#ffffff" },
  "files": {}
}
EXCALIDRAWFILE

# Build crit binary outside the fixture dir (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# Isolate from user's ~/.crit.config.json (and USERPROFILE on Windows).
e2e_export_fake_home "$DIR"

# What `crit arch.excalidraw` runs: a preview session on the scene.
e2e_serve "$CRIT_BIN" _serve --no-open --port "$PORT" --preview-file "$DIR/arch.excalidraw"
