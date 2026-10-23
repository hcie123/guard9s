"""Static contract and ZIP structure tests for the documented WorkBuddy expert format."""
from __future__ import annotations

import importlib.util
import json
import re
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
    def test_github_artifact_job_is_limited_to_pr_7_and_uses_runner_temp(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        self.assertIn("workbuddy-expert-package:", workflow)
        self.assertIn("github.event_name == 'pull_request' && github.event.pull_request.number == 7", workflow)
        self.assertIn('"${RUNNER_TEMP}/k8s-ai-ops-diagnosis-expert.zip"', workflow)
        self.assertIn("name: k8s-ai-ops-diagnosis-expert", workflow)
        self.assertIn("retention-days: 7", workflow)
        self.assertNotIn("dist/k8s-ai-ops-diagnosis-expert.zip", workflow)

    def test_documented_package_files_validate(self):
        self.assertEqual([], package_tool.validate())

    def test_agent_declares_offline_read_only_safety_and_guard9s_limits(self):
        agent_path = package_tool.PACKAGE / "agents/k8s-ai-ops-diagnosis-expert.md"
        text = agent_path.read_text(encoding="utf-8")
        for phrase in (
            "不连接 Kubernetes API", "不运行 `kubectl`", "UNKNOWN（证据不足）",
            "FailedScheduling", "PDB", "requests", "CRITICAL", "修复建议",
            "不构成 guard9s 程序输出", "kubectl get", "kubectl describe",
            "故障结论", "可能原因", "## 证据", "只读排查命令", "风险提示",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, text)
        self.assertIn("不生成", text)
        for mutation in ("`apply`", "`patch`", "`delete`", "`drain`", "`exec`", "`port-forward`"):
            # Dangerous verbs are explicitly forbidden in context.
            self.assertIn(mutation, text)

    def test_synthetic_fault_corpus_has_required_scenarios_and_safe_expectations(self):
        corpus_path = ROOT / "tests/scenarios/k8s-offline-diagnosis.json"
        corpus = json.loads(corpus_path.read_text(encoding="utf-8"))
        scenarios = corpus["scenarios"]
        ids = {item["id"] for item in scenarios}
        expected = {
            "normal-snapshot", "pod-pending-cpu", "pod-pending-affinity", "crashloop-probe",
            "imagepull-backoff", "node-notready", "oomkilled-evidence", "cilium-networkpolicy",
            "cpu-pressure", "memory-pressure", "disk-pressure", "pvc-pending-waitforconsumer",
            "scheduling-taint", "insufficient-evidence", "sensitive-redaction",
        }
        self.assertGreaterEqual(len(scenarios), 10)
        self.assertTrue(expected <= ids)
        allowed = re.compile(r"^(?:kubectl (?:get|describe|top|logs)(?:\s|$)|cilium status(?:\s|$)|df(?:\s|$))")
        for item in scenarios:
            with self.subTest(scenario=item["id"]):
                for field in ("category", "input", "expected_conclusion", "required_evidence", "expected_unknowns", "safe_commands"):
                    self.assertIn(field, item)
                self.assertTrue(item["required_evidence"])
                self.assertTrue(item["expected_unknowns"])
                for command in item["safe_commands"]:
                    self.assertRegex(command, allowed)
                    self.assertNotRegex(command, r"(?i)\b(apply|patch|delete|edit|exec|drain|cordon|restart|rollout)\b")
        redaction = next(item for item in scenarios if item["id"] == "sensitive-redaction")
        self.assertTrue(redaction["sensitive_markers"])
        for marker in redaction["sensitive_markers"]:
            self.assertNotIn(marker, redaction["expected_response"])
        self.assertIn("[REDACTED]", redaction["expected_response"])

    def test_fault_reference_covers_all_named_diagnoses_and_report_contract(self):
        ref = package_tool.PACKAGE / "skills/k8s-offline-diagnostics/references/fault-patterns.md"
        text = ref.read_text(encoding="utf-8")
        for term in ("Pending", "CrashLoopBackOff", "ImagePullBackOff", "Node NotReady", "OOMKilled", "Cilium", "CPU", "MemoryPressure", "DiskPressure", "PVC Pending", "FailedScheduling", "UNKNOWN", "只读核查", "建议/风险"):
            with self.subTest(term=term):
                self.assertIn(term, text)

    def test_license_copyright_and_runtime_dependency_scope(self):
        package = package_tool.PACKAGE
        manifest = json.loads((package / ".codebuddy-plugin/plugin.json").read_text(encoding="utf-8"))
        license_text = (package / "LICENSE").read_text(encoding="utf-8")
        notice = (package / "NOTICE").read_text(encoding="utf-8")
        self.assertEqual("Apache-2.0", manifest["license"])
        self.assertIn("Apache License", license_text)
        self.assertIn("Copyright 2026 hcie123 and contributors", notice)
        self.assertIn("no bundled runtime or third-party software dependencies", notice)
        self.assertFalse((package / ".mcp.json").exists())
        self.assertFalse(any(package.rglob("*.go")))
        self.assertFalse(any(package.rglob("*.exe")))

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
                self.assertIn(prefix + "skills/k8s-offline-diagnostics/references/fault-patterns.md", names)
                self.assertIn(prefix + "LICENSE", names)
                self.assertIn(prefix + "NOTICE", names)
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
