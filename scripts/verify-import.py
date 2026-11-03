#!/usr/bin/env python3
"""Compare a native Memos export with a Vrac export without printing note content."""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import zipfile


def verify(source: Path, exported: Path, attachments: Path) -> dict:
    data = json.loads(exported.read_text())
    notes = data if isinstance(data, list) else data.get("notes", [])
    by_source = {note.get("sourceId") or note["id"]: note for note in notes}
    failures = []
    checked = 0
    attachment_count = 0
    states = {"active": 0, "archived": 0}
    with zipfile.ZipFile(source) as archive:
        manifest = json.loads(archive.read("manifest.json"))
        records = [name for name in archive.namelist() if name.startswith("memos/") and name.endswith(".json")]
        for name in records:
            original = json.loads(archive.read(name))
            uid = original["uid"]
            imported = by_source.get(uid) or by_source.get("memos/" + uid)
            if imported is None:
                failures.append({"id": uid, "field": "missing note"})
                continue
            checked += 1
            content = archive.read(original["contentPath"]).decode("utf-8")
            state = "archived" if original.get("state") == "ARCHIVED" else "active"
            states[state] += 1
            expected = {"content": content, "createdAt": original["createTime"], "updatedAt": original["updateTime"], "state": state, "pinned": original.get("pinned", False)}
            for field, value in expected.items():
                if imported.get(field) != value:
                    failures.append({"id": uid, "field": field})
            if not set(original.get("tags", [])).issubset(set(imported.get("tags", []))):
                failures.append({"id": uid, "field": "tags"})
            actual_files = {file["id"]: file for file in imported.get("attachments", [])}
            for file in original.get("attachments", []):
                attachment_count += 1
                imported_file = actual_files.get(file["uid"]) or actual_files.get("attachments/" + file["uid"])
                if not imported_file:
                    failures.append({"id": uid, "field": "attachment metadata"})
                file_path = attachments / file["uid"] / file["filename"]
                expected_hash = file.get("sha256") or hashlib.sha256(archive.read(file["path"])).hexdigest()
                if not file_path.is_file() or hashlib.sha256(file_path.read_bytes()).hexdigest() != expected_hash:
                    failures.append({"id": file["uid"], "field": "attachment sha256"})
        if len(records) != manifest["counts"]["memos"]:
            failures.append({"field": "source manifest count"})
    return {"passed": not failures, "sourceNotes": len(records), "verifiedNotes": checked, "states": states, "verifiedAttachments": attachment_count, "failures": failures}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--export", required=True, type=Path)
    parser.add_argument("--attachments", required=True, type=Path)
    args = parser.parse_args()
    result = verify(args.source, args.export, args.attachments)
    print(json.dumps(result, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
