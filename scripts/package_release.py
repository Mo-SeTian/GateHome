#!/usr/bin/env python3
"""Create a complete, credential-free release tree and a checked update ZIP."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
version = (ROOT / "VERSION").read_text().strip()
if not re.fullmatch(r"(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})", version):
    raise SystemExit("Invalid VERSION")

# Generated artifacts and private runtime data are never part of a release.
excluded = {".git", ".local", ".agents", "skills-lock.json", "config", "log", "data", "dist", "版本", "更新版本", "__pycache__", ".DS_Store", "state.json", "calls.jsonl", "backup.enc", "restore-staged.json", "rollback.json", "ui-redesign-2026-10-08", "waf-0.0.16", "security-records-0.0.17", "service-discovery-0.0.18", "route-images-backup-0.0.19", "design-qa.md"}
def private(path):
    return any(part in excluded or part.startswith(".env") for part in path.parts) or path.suffix in {".key", ".pem", ".test"}

versions = ROOT / "版本"
updates = ROOT / "更新版本"
versions.mkdir(exist_ok=True)
updates.mkdir(exist_ok=True)
destination = versions / version
with tempfile.TemporaryDirectory(prefix=".release-", dir=versions) as temp:
    stage = Path(temp)
    for source in ROOT.iterdir():
        if private(Path(source.name)) or source.name in {"manifest.json", "SHA256SUMS"}:
            continue
        if source.is_symlink():
            raise SystemExit("Release source contains a symlink")
        if source.is_file():
            shutil.copy2(source, stage / source.name)
        elif source.is_dir():
            for item in source.rglob("*"):
                relative = item.relative_to(ROOT)
                if private(relative):
                    continue
                if item.is_symlink():
                    raise SystemExit("Release source contains a symlink")
                target = stage / relative
                if item.is_dir():
                    target.mkdir(parents=True, exist_ok=True)
                elif item.is_file():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(item, target)
    (stage / "dist").mkdir()
    checksums = []
    for arch in ("amd64", "arm64"):
        name = "gatehouse-linux-" + arch
        binary = ROOT / "dist" / name
        if not binary.is_file():
            raise SystemExit("Run make linux before packaging")
        shutil.copy2(binary, stage / "dist" / name)
        checksums.append(hashlib.sha256(binary.read_bytes()).hexdigest() + "  dist/" + name)
    (stage / "SHA256SUMS").write_text("\n".join(checksums) + "\n")
    os.chmod(stage / "install.sh", 0o755)
    files = {}
    for item in sorted(stage.rglob("*")):
        if item.is_file():
            content = item.read_bytes()
            files[item.relative_to(stage).as_posix()] = {"size": len(content), "sha256": hashlib.sha256(content).hexdigest()}
    manifest = {"format": "gatehouse-update-v1", "version": version, "files": files}
    (stage / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    if destination.exists():
        shutil.rmtree(destination)  # Only this script's version output is replaced.
    shutil.copytree(stage, destination)

archive = updates / ("gatehouse-" + version + "-update.zip")
temporary = archive.with_suffix(".pending")
with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as z:
    for item in sorted(destination.rglob("*")):
        if item.is_file():
            z.write(item, item.relative_to(destination).as_posix())
temporary.replace(archive)
print("Version directory:", destination.relative_to(ROOT))
print("Update ZIP:", archive.relative_to(ROOT))

# Architecture-specific packages keep curl installs independent of a compiler.
assets = [archive]
for arch in ("amd64", "arm64"):
    asset = updates / ("gatehome-linux-" + arch + ".tar.gz")
    with tempfile.TemporaryDirectory(prefix=".install-", dir=updates) as temp:
        package = Path(temp)
        for name in ("VERSION", "install.sh", "deploy/gatehouse.service", "dist/gatehouse-linux-" + arch):
            target = package / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(destination / name, target)
        checksum = next(line for line in checksums if line.endswith("dist/gatehouse-linux-" + arch))
        (package / "SHA256SUMS").write_text(checksum + "\n")
        with tarfile.open(asset.with_suffix(".pending"), "w:gz") as tar:
            for item in sorted(package.rglob("*")):
                if item.is_file():
                    tar.add(item, arcname=item.relative_to(package).as_posix())
        asset.with_suffix(".pending").replace(asset)
    assets.append(asset)
    print("Linux install archive:", asset.relative_to(ROOT))
(updates / "gatehome-SHA256SUMS").write_text("".join(hashlib.sha256(asset.read_bytes()).hexdigest() + "  " + asset.name + "\n" for asset in assets))
