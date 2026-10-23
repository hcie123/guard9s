"""Static contract and ZIP structure tests for the documented WorkBuddy expert format."""
from __future__ import annotations

import importlib.util
import tempfile
import unittest
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/package_workbuddy_expert.py"
spec = importlib.util.spec_from_file_location("package_workbuddy_expert", SCRIPT)
package_tool = importlib.util.module_from_spec(spec)
assert spec and spec.loader
spec.loader.exec_module(package_tool)


class ExpertPackageTests(unittest.TestCase):
    def test_documented_package_files_validate(self):
        self.assertEqual([], package_tool.validate())

    def test_agent_declares_offline_read_only_safety_and_guard9s_limits(self):
        agent_path = package_tool.PACKAGE / "agents/k8s-ai-ops-diagnosis-expert.md"
        text = agent_path.read_text(encoding="utf-8")
        for phrase in (
            "不连接 Kubernetes API", "不运行 `kubectl`", "UNKNOWN（证据不足）",
            "FailedScheduling", "PDB", "requests", "CRITICAL", "修复方向",
            "不构成 guard9s 程序输出", "kubectl get", "kubectl describe",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, text)
        self.assertIn("不生成", text)
        for mutation in ("`apply`", "`patch`", "`delete`", "`drain`", "`exec`", "`port-forward`"):
            # Dangerous verbs are explicitly forbidden in context.
            self.assertIn(mutation, text)

    def test_build_zip_layout_size_paths_and_contents(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "expert.zip"
            self.assertEqual(0, package_tool.build(output))
            self.assertLess(output.stat().st_size, package_tool.MAX_ZIP_BYTES)
            with zipfile.ZipFile(output) as archive:
                names = archive.namelist()
                prefix = package_tool.NAME + "/"
                self.assertTrue(names)
                self.assertTrue(all(name.startswith(prefix) for name in names))
                self.assertIn(prefix + ".codebuddy-plugin/plugin.json", names)
                self.assertIn(prefix + "agents/" + package_tool.NAME + ".md", names)
                self.assertIn(prefix + "avatars/expert.png", names)
                self.assertIn(prefix + "skills/k8s-offline-diagnostics/SKILL.md", names)
                self.assertIn(prefix + "LICENSE", names)
                self.assertFalse(any(Path(name).is_absolute() or ".." in Path(name).parts for name in names))
                manifest = __import__("json").loads(archive.read(prefix + ".codebuddy-plugin/plugin.json"))
                self.assertEqual(package_tool.NAME, manifest["agentName"])
                self.assertIn("不连接 Kubernetes API".encode(), archive.read(prefix + "agents/" + package_tool.NAME + ".md"))

    def test_missing_required_file_fails_validation(self):
        package = package_tool.PACKAGE
        # Validate via an isolated copy without mutating the source tree.
        import shutil
        with tempfile.TemporaryDirectory() as temp:
            copied = Path(temp) / package_tool.NAME
            shutil.copytree(package, copied)
            (copied / "agents/k8s-ai-ops-diagnosis-expert.md").unlink()
            errors = package_tool.validate(copied)
            self.assertTrue(any("missing required file" in error for error in errors))


if __name__ == "__main__":
    unittest.main()
