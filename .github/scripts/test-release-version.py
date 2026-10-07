import json
import os
import subprocess
import tempfile
import textwrap
from pathlib import Path

workflow = (Path(__file__).resolve().parents[1] / "workflows/release.yml").read_text()
step = workflow.split("      - name: Determine version\n", 1)[1]
script = textwrap.dedent(step.split("        run: |\n", 1)[1].split("\n  build:", 1)[0])

with tempfile.TemporaryDirectory(prefix="silo-release-test-") as directory:
    root = Path(directory)
    repository = root / "repository"
    repository.mkdir()
    subprocess.run(["git", "init", "--bare", str(root / "remote")], check=True, capture_output=True)
    subprocess.run(["git", "init", str(repository)], check=True, capture_output=True)
    environment = {
        **os.environ,
        "GIT_AUTHOR_NAME": "Release test",
        "GIT_AUTHOR_EMAIL": "release-test@example.invalid",
        "GIT_COMMITTER_NAME": "Release test",
        "GIT_COMMITTER_EMAIL": "release-test@example.invalid",
        "GITHUB_REF": "refs/heads/main",
        "GITHUB_REF_NAME": "main",
        "GITHUB_OUTPUT": str(root / "output"),
    }
    (repository / "manifest.json").write_text(json.dumps({"version": "0.1.0"}))
    for arguments in [["add", "manifest.json"], ["commit", "-m", "test fixture"], ["remote", "add", "origin", str(root / "remote")]]:
        subprocess.run(["git", *arguments], cwd=repository, env=environment, check=True, capture_output=True)
    original = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repository).decode().strip()

    for run, ref, expected in [("100", "refs/heads/main", "0.1.0"), ("100", "refs/heads/main", "0.1.0"), ("101", "refs/heads/main", "0.1.1"), ("101", "refs/tags/v0.1.1", "0.1.1")]:
        subprocess.run(["git", "reset", "--hard", original], cwd=repository, check=True, capture_output=True)
        (root / "output").write_text("")
        environment.update(GITHUB_RUN_ID=run, GITHUB_REF=ref, GITHUB_REF_NAME=ref.rsplit("/", 1)[-1])
        subprocess.run(["bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script], cwd=repository, env=environment, check=True, capture_output=True)
        assert (root / "output").read_text() == f"version={expected}\ntag=v{expected}\n", (run, ref)

    tags = subprocess.check_output(["git", "tag", "-l"], cwd=repository).decode().splitlines()
    assert tags == ["v0.1.0", "v0.1.1"], tags

print("Release version checks passed: initial version, retry, next run and explicit tag")
