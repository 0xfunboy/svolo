#!/usr/bin/env python3
"""Regenerate the builtin tool schema without starting user sessions."""
from pathlib import Path
import json, subprocess
root = Path(__file__).resolve().parents[1]
raw = subprocess.check_output(["go", "run", "./cmd/svolo-core", "catalog"], cwd=root / "core")
value = json.loads(raw)
assert value["product"] == "Svolo" and value["tools"]
(root / "schemas" / "tools.json").write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")
print(f"Wrote {len(value['tools'])} builtin tools.")
