#!/usr/bin/env python3
"""Validate and package the WorkBuddy expert using its official documented layout."""
from __future__ import annotations

import argparse
import json
import re
import sys
import zipfile
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = ROOT / "workbuddy" / "k8s-ai-ops-diagnosis-expert"
NAME = "k8s-ai-ops-diagnosis-expert"
MAX_ZIP_BYTES = 20 * 1024 * 1024
MAX_AVATAR_BYTES = 500 * 1024
REQUIRED_FILES = (
    ".codebuddy-plugin/plugin.json",
    "agents/k8s-ai-ops-diagnosis-expert.md",
    "avatars/expert.png",
    "skills/k8s-offline-diagnostics/SKILL.md",
    "LICENSE",
    "NOTICE",
    "README.md",
)


def validate(package: Path = PACKAGE) -> list[str]:
    errors: list[str] = []
    for rel in REQUIRED_FILES:
        path = package / rel
        if not path.is_file():
            errors.append(f"missing required file: {rel}")
    if errors:
        return errors

    try:
        manifest = json.loads((package / REQUIRED_FILES[0]).read_text(encoding="utf-8"))
    except (UnicodeError, json.JSONDecodeError) as exc:
        return [f"invalid plugin.json: {exc}"]

    required = {
        "name", "version", "description", "author", "agents", "expertType", "agentName",
        "displayName", "profession", "displayDescription", "avatar", "categoryId",
        "defaultInitPrompt", "plugin", "tags", "quickPrompts",
    }
    absent = sorted(required - manifest.keys())
    if absent:
        errors.append("missing plugin.json fields: " + ", ".join(absent))
    if manifest.get("name") != NAME or manifest.get("plugin") != NAME:
        errors.append("plugin.json name and plugin must match the package id")
    if manifest.get("expertType") != "agent":
        errors.append('expertType must be "agent"')
    if manifest.get("agentName") != NAME:
        errors.append("agentName must match the agent markdown file stem")
    if manifest.get("agents") != [f"./agents/{NAME}.md"]:
        errors.append("agents must point to the documented Agent definition path")
    if manifest.get("skills") != ["./skills/k8s-offline-diagnostics"]:
        errors.append("skills must point to the documented skill directory")
    if manifest.get("categoryId") not in {f"{i:02d}-{n}" for i, n in [
        (1, "ProductDesign"), (2, "Engineering"), (3, "GameSpatial"), (4, "DataAI"),
        (5, "MarketingGrowth"), (6, "ContentCreative"), (7, "SalesCommerce"),
        (8, "FinanceInvestment"), (9, "OperationsHR"), (10, "ProjectQuality"),
        (11, "SecurityCompliance"), (12, "IndustryConsultant"), (13, "TencentZone"),
        (14, "WorldWise"), (15, "Education")
    ]}:
        errors.append("categoryId is not in the official category list")

    for key in ("displayName", "profession", "displayDescription", "defaultInitPrompt"):
        value = manifest.get(key)
        if not isinstance(value, dict) or not value.get("zh") or not value.get("en"):
            errors.append(f"{key} must contain non-empty zh and en strings")
    zh_desc = manifest.get("displayDescription", {}).get("zh", "")
    han_count = len(re.findall(r"[\u4e00-\u9fff]", zh_desc))
    if not 40 <= han_count <= 50:
        errors.append(f"displayDescription.zh must contain 40–50 Chinese characters (found {han_count})")
    if len(manifest.get("tags", [])) != 3:
        errors.append("tags must contain exactly 3 bilingual entries")
    if len(manifest.get("quickPrompts", [])) != 3:
        errors.append("quickPrompts must contain exactly 3 bilingual entries")
    if manifest.get("defaultInitPrompt") != (manifest.get("quickPrompts") or [None])[0]:
        errors.append("defaultInitPrompt must equal quickPrompts[0]")
    for field in ("tags", "quickPrompts"):
        for i, entry in enumerate(manifest.get(field, [])):
            if not isinstance(entry, dict) or not entry.get("zh") or not entry.get("en"):
                errors.append(f"{field}[{i}] must contain zh and en strings")
    for field in ("avatar",):
        p = PurePosixPath(manifest.get(field, ""))
        if p.is_absolute() or ".." in p.parts or not (package / p).is_file():
            errors.append(f"{field} must point to an existing relative file")

    agent_rel = f"agents/{NAME}.md"
    agent = (package / agent_rel).read_text(encoding="utf-8")
    if not agent.startswith("---\n") or "\n---\n" not in agent[4:]:
        errors.append("Agent definition must start with YAML frontmatter")
    else:
        frontmatter = agent.split("\n---\n", 1)[0][4:]
        for expected in (f"name: {NAME}", "description:", "displayName:", "profession:", "k8s-offline-diagnostics"):
            if expected not in frontmatter:
                errors.append(f"Agent frontmatter missing {expected!r}")
    avatar = package / "avatars/expert.png"
    raw = avatar.read_bytes()
    if len(raw) > MAX_AVATAR_BYTES:
        errors.append("avatar exceeds the official 500 KiB limit")
    if len(raw) < 24 or raw[:8] != b"\x89PNG\r\n\x1a\n" or raw[12:16] != b"IHDR":
        errors.append("avatar must be a valid PNG file")
    elif int.from_bytes(raw[16:20], "big") != 512 or int.from_bytes(raw[20:24], "big") != 512:
        errors.append("avatar must be 512x512 pixels per the official expert specification")

    files = sorted(p for p in package.rglob("*") if p.is_file())
    for markdown in (p for p in files if p.suffix.lower() == ".md"):
        content = markdown.read_text(encoding="utf-8")
        for target in re.findall(r"\[[^\]]*\]\(([^)]+)\)", content):
            target = target.split("#", 1)[0]
            if not target or "://" in target or target.startswith("mailto:"):
                continue
            rel = PurePosixPath(target)
            if rel.is_absolute() or ".." in rel.parts or not (markdown.parent / Path(*rel.parts)).exists():
                errors.append(f"broken or escaping Markdown link in {markdown.relative_to(package)}: {target}")
    for path in files:
        if path.is_symlink():
            errors.append(f"symlinks are not allowed in expert ZIP: {path.relative_to(package)}")
        rel = path.relative_to(package).as_posix()
        if rel.endswith((".pem", ".key", ".kubeconfig")) or rel in {".env", "kubeconfig"}:
            errors.append(f"credential-like file is not allowed: {rel}")
    return errors


def build(output: Path, package: Path = PACKAGE) -> int:
    errors = validate(package)
    if errors:
        for error in errors:
            print("ERROR:", error, file=sys.stderr)
        return 2
    output.parent.mkdir(parents=True, exist_ok=True)
    if output.exists():
        output.unlink()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in sorted(p for p in package.rglob("*") if p.is_file()):
            rel = path.relative_to(package.parent).as_posix()
            info = zipfile.ZipInfo(rel, date_time=(2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    size = output.stat().st_size
    if size >= MAX_ZIP_BYTES:
        output.unlink(missing_ok=True)
        print(f"ERROR: ZIP is {size} bytes; limit is strictly below 20 MiB", file=sys.stderr)
        return 2
    print(f"PASS: validated official documented package structure; ZIP={output} ({size} bytes < 20 MiB)")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / f"{NAME}.zip")
    parser.add_argument("--validate-only", action="store_true")
    args = parser.parse_args()
    errors = validate()
    if errors:
        for error in errors:
            print("ERROR:", error, file=sys.stderr)
        return 2
    if args.validate_only:
        print("PASS: expert files satisfy local documented-format checks")
        return 0
    return build(args.output)


if __name__ == "__main__":
    raise SystemExit(main())
