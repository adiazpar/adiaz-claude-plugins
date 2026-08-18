import hashlib
import json
import os
import platform
import shutil
import stat
import struct
import subprocess
import tempfile
import unittest
from pathlib import Path

from tests.re_discipline_package_audit import declared_plugin_version


ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT
DECLARED_VERSION = declared_plugin_version(ROOT)
BIN_ROOT = PLUGIN / "knowledge" / "bin"
EXPECTED_PLATFORMS = {
    ("windows", "amd64"): "re-discipline-knowledge.exe",
    ("windows", "arm64"): "re-discipline-knowledge.exe",
    ("linux", "amd64"): "re-discipline-knowledge",
    ("linux", "arm64"): "re-discipline-knowledge",
    ("darwin", "amd64"): "re-discipline-knowledge",
    ("darwin", "arm64"): "re-discipline-knowledge",
}
EXPECTED_TOOLS = [
    "state",
    "query",
    "read",
    "trace",
    "context_pack_materialize",
    "manager_apply",
    "campaign_merge_plan",
    "curation_submit",
    "closure_apply",
    "normalization_queue",
    "migrate_project",
]


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def copy_or_link(source: str, target: str) -> str:
    try:
        os.link(source, target)
        return target
    except OSError:
        return shutil.copy2(source, target)


def snapshot_tree(root: Path) -> dict[str, str]:
    if not root.exists():
        return {}
    return {
        path.relative_to(root).as_posix(): sha256(path)
        for path in sorted(root.rglob("*"))
        if path.is_file()
    }


def parse_sums(path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        if len(parts) < 2:
            raise AssertionError(f"malformed checksum line: {raw_line!r}")
        digest = parts[0].lower()
        relative = parts[-1].lstrip("*").replace("\\", "/")
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
            raise AssertionError(f"malformed checksum digest: {raw_line!r}")
        if relative in result:
            raise AssertionError(f"duplicate checksum path: {relative}")
        result[relative] = digest
    return result


def command_path(plugin_root: Path, command: str) -> Path:
    expanded = command.replace("${CLAUDE_PLUGIN_ROOT}", str(plugin_root))
    path = Path(expanded)
    if not path.is_absolute():
        path = plugin_root / path
    if os.name == "nt" and not path.suffix:
        for suffix in (".exe", ".cmd", ".bat"):
            candidate = Path(str(path) + suffix)
            if candidate.exists():
                return candidate
    return path


def substitute_plugin_root(value: str, plugin_root: Path) -> str:
    return value.replace("${CLAUDE_PLUGIN_ROOT}", str(plugin_root))


class ReDisciplineKnowledgeRuntimeIntegrationTests(unittest.TestCase):
    maxDiff = None

    def test_dispatch_binds_manager_retained_context_pack_digest_across_routes(
        self,
    ) -> None:
        dispatch = (
            PLUGIN / "templates" / "project" / "dispatch.ps1"
        ).read_text(encoding="utf-8")
        self.assertIn("[string]$ExpectedContextPackDigest", dispatch)
        self.assertIn(
            "verify-pack --input $contextPack --expected-digest "
            "$ExpectedContextPackDigest",
            " ".join(dispatch.split()),
        )
        self.assertIn(
            "[ValidatePattern('^sha256:[0-9a-f]{64}$')]",
            dispatch,
        )
        self.assertGreaterEqual(
            dispatch.count("$ExpectedContextPackDigest"),
            4,
            "the retained digest must reach validation, verification, and drafter text",
        )

        delegate = (
            PLUGIN / "skills" / "delegate" / "SKILL.md"
        ).read_text(encoding="utf-8").lower()
        hire_agent = (
            PLUGIN / "skills" / "hire-agent" / "SKILL.md"
        ).read_text(encoding="utf-8").lower()
        contract = (
            PLUGIN
            / "templates"
            / "project"
            / "external-drafter-contract.md"
        ).read_text(encoding="utf-8").lower()
        for label, text in (
            ("delegate skill", delegate),
            ("hire-agent skill", hire_agent),
            ("drafter contract", contract),
        ):
            self.assertIn("digest", text, label)
            self.assertTrue(
                "exact" in text or "verify" in text,
                f"{label} does not require exact context-pack verification",
            )
            self.assertIn("mismatch", text, label)
            self.assertTrue(
                "block" in text or "stop" in text,
                f"{label} does not fail closed on a context-pack digest mismatch",
            )
        for label, text in (
            ("delegate skill", delegate),
            ("hire-agent skill", hire_agent),
        ):
            self.assertIn("retained digest", text, label)
            self.assertIn("packaged runtime", text, label)

        dispatch_readme = (
            PLUGIN / "templates" / "project" / "agents-README.md"
        ).read_text(encoding="utf-8").lower()
        self.assertEqual(
            dispatch_readme.count("-expectedcontextpackdigest"),
            1,
        )

    def test_drafter_context_treats_retrieved_instructions_as_untrusted_data(
        self,
    ) -> None:
        contract = (
            PLUGIN
            / "templates"
            / "project"
            / "external-drafter-contract.md"
        ).read_text(encoding="utf-8").lower()
        dispatch = (
            PLUGIN / "templates" / "project" / "dispatch.ps1"
        ).read_text(encoding="utf-8").lower()

        for label, text in (("drafter contract", contract), ("dispatch", dispatch)):
            compact = " ".join(text.split())
            normalized = compact.replace("-", " ")
            for token in ("constraints", "cards", "expansion handles", "as data"):
                self.assertIn(token, compact, label)
            self.assertIn("context", compact, label)
            self.assertIn("digest", compact, label)
            self.assertIn("brief", compact, label)
            self.assertIn("drafter contract", normalized, label)

    def assert_binary_identity(self, path: Path, goos: str, goarch: str) -> None:
        body = path.read_bytes()
        self.assertGreater(len(body), 1024 * 1024, f"{path} is not a packaged native binary")
        if goos == "windows":
            self.assertEqual(body[:2], b"MZ", path)
            pe_offset = struct.unpack_from("<I", body, 0x3C)[0]
            self.assertEqual(body[pe_offset : pe_offset + 4], b"PE\0\0", path)
            machine = struct.unpack_from("<H", body, pe_offset + 4)[0]
            self.assertEqual(machine, 0x8664 if goarch == "amd64" else 0xAA64, path)
        elif goos == "linux":
            self.assertEqual(body[:4], b"\x7fELF", path)
            machine = struct.unpack_from("<H", body, 18)[0]
            self.assertEqual(machine, 62 if goarch == "amd64" else 183, path)
        else:
            self.assertEqual(body[:4], b"\xcf\xfa\xed\xfe", path)
            cpu_type = struct.unpack_from("<I", body, 4)[0]
            self.assertEqual(
                cpu_type,
                0x01000007 if goarch == "amd64" else 0x0100000C,
                path,
            )

    def test_complete_native_matrix_manifest_and_checksums(self) -> None:
        manifest_path = BIN_ROOT / "manifest.json"
        sums_path = BIN_ROOT / "SHA256SUMS"
        schema_path = (
            PLUGIN
            / "knowledge"
            / "schemas"
            / "runtime-package-manifest.schema.json"
        )
        self.assertTrue(manifest_path.is_file(), "missing knowledge/bin/manifest.json")
        self.assertTrue(sums_path.is_file(), "missing knowledge/bin/SHA256SUMS")
        self.assertTrue(schema_path.is_file(), "missing runtime package manifest schema")
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        schema = json.loads(schema_path.read_text(encoding="utf-8"))
        sums = parse_sums(sums_path)
        artifact_hashes: set[str] = set()

        self.assertEqual(
            set(manifest),
            {
                "$schema",
                "schemaVersion",
                "runtime",
                "build",
                "targets",
                "launchers",
                "sharedAssets",
                "notices",
            },
        )
        self.assertEqual(
            manifest["$schema"], "../schemas/runtime-package-manifest.schema.json"
        )
        self.assertEqual(manifest["schemaVersion"], 1)
        self.assertEqual(
            schema.get("$id"),
            "plugin://re-discipline/schemas/runtime-package-manifest.schema.json",
        )
        self.assertEqual(manifest["runtime"]["name"], "re-discipline-knowledge")
        self.assertEqual(manifest["runtime"]["version"], DECLARED_VERSION)
        self.assertRegex(manifest["runtime"]["buildId"], r"^sha256:[0-9a-f]{64}$")
        self.assertFalse(manifest["build"]["cgoEnabled"])
        self.assertRegex(
            manifest["build"]["goToolchain"], r"^go[0-9]+\.[0-9]+\.[0-9]+$"
        )
        self.assertEqual(
            manifest["build"]["targetOrder"],
            "windows-amd64,windows-arm64,linux-amd64,linux-arm64,"
            "darwin-amd64,darwin-arm64",
        )
        self.assertIn("-trimpath", manifest["build"]["flags"])
        self.assertIn("-buildvcs=false", manifest["build"]["flags"])
        self.assertEqual(
            manifest["build"]["environment"],
            [
                "CGO_ENABLED=0",
                "GOAMD64=v1",
                "GOARM64=v8.0",
                "GOENV=off",
                "GOEXPERIMENT=",
                "GOFIPS140=off",
                "GOFLAGS=-mod=readonly",
                "GOWORK=off",
            ],
        )

        targets = manifest["targets"]
        self.assertEqual(len(targets), len(EXPECTED_PLATFORMS))
        expected_sums: dict[str, str] = {}
        expected_bin_files = {"manifest.json", "SHA256SUMS"}
        for row, ((goos, goarch), filename) in zip(
            targets, EXPECTED_PLATFORMS.items(), strict=True
        ):
            relative = f"{goos}-{goarch}/{filename}"
            self.assertEqual(
                set(row), {"kind", "goos", "goarch", "path", "sha256", "size", "mode"}
            )
            self.assertEqual(
                row,
                {
                    "kind": "runtime",
                    "goos": goos,
                    "goarch": goarch,
                    "path": relative,
                    "sha256": row["sha256"],
                    "size": row["size"],
                    "mode": "0644" if goos == "windows" else "0755",
                },
            )
            artifact = BIN_ROOT / relative
            self.assertTrue(artifact.is_file(), f"missing packaged artifact {relative}")
            self.assert_binary_identity(artifact, goos, goarch)
            if os.name != "nt":
                expected_mode = 0o644 if goos == "windows" else 0o755
                self.assertEqual(
                    stat.S_IMODE(artifact.stat().st_mode),
                    expected_mode,
                    f"unstable POSIX mode for {relative}",
                )
            digest = sha256(artifact)
            artifact_hashes.add(digest)
            self.assertEqual(row["size"], artifact.stat().st_size)
            self.assertEqual(row["sha256"], "sha256:" + digest)
            expected_sums[relative] = digest
            expected_bin_files.add(relative)

        self.assertEqual(len(artifact_hashes), len(EXPECTED_PLATFORMS))

        launchers = manifest["launchers"]
        self.assertEqual(len(launchers), 2)
        self.assertEqual(
            (launchers[0]["kind"], launchers[0]["path"], launchers[0]["mode"]),
            ("posix-dispatch", "re-discipline-knowledge", "0755"),
        )
        self.assertEqual(
            (
                launchers[1]["kind"],
                launchers[1]["goos"],
                launchers[1]["goarch"],
                launchers[1]["path"],
                launchers[1]["mode"],
            ),
            (
                "windows-architecture-dispatch",
                "windows",
                "amd64",
                "re-discipline-knowledge.exe",
                "0644",
            ),
        )
        posix_launcher = BIN_ROOT / launchers[0]["path"]
        windows_dispatcher = BIN_ROOT / launchers[1]["path"]
        self.assertTrue(posix_launcher.is_file(), "missing POSIX platform launcher")
        self.assertTrue(windows_dispatcher.is_file(), "missing Windows dispatcher")
        self.assert_binary_identity(windows_dispatcher, "windows", "amd64")
        extensionless_command = command_path(
            PLUGIN, "knowledge/bin/re-discipline-knowledge"
        )
        if os.name == "nt":
            self.assertEqual(
                extensionless_command,
                windows_dispatcher,
                "Windows extensionless command resolution did not select sibling .exe",
            )
        else:
            self.assertEqual(extensionless_command, posix_launcher)
            self.assertTrue(
                posix_launcher.stat().st_mode & stat.S_IXUSR,
                "POSIX platform launcher is not executable",
            )
            self.assertEqual(
                stat.S_IMODE(windows_dispatcher.stat().st_mode),
                0o644,
                "Windows-only dispatcher has an unstable POSIX mode",
            )
        for row in launchers:
            path = BIN_ROOT / row["path"]
            digest = sha256(path)
            self.assertEqual(row["size"], path.stat().st_size)
            self.assertEqual(row["sha256"], "sha256:" + digest)
            expected_sums[row["path"]] = digest
            expected_bin_files.add(row["path"])

        shared_assets = manifest["sharedAssets"]
        knowledge_root = PLUGIN / "knowledge"

        def shared_asset_kind(relative: str) -> str:
            if relative in {
                "evals/conformance/cases.json",
                "evals/conformance/finding-cases.json",
            }:
                return "benchmark-cases"
            if relative == "evals/conformance/lane-ablation-decision.json":
                return "lane-ablation-decision"
            if relative == "evals/conformance/lane-ablation-report.json":
                return "lane-ablation-report"
            if relative == "evals/conformance/project-lane-ablation.json":
                return "project-lane-ablation-measurement"
            if (
                relative.startswith("evals/conformance/evidence/")
                and relative.lower().endswith(".zip")
            ):
                return "lane-ablation-evidence-archive"
            if (
                relative.startswith("evals/conformance/fixture/")
                and relative.lower().endswith((".md", ".json", ".jsonc"))
            ):
                return "benchmark-fixture"
            if relative == "models/manifest.json":
                return "model-manifest"
            if relative.startswith("models/specs/") and relative.lower().endswith(
                ".json"
            ):
                return "model-specification"
            if relative.startswith(
                "models/artifacts/"
            ) and relative.lower().endswith(".bin"):
                return "shared-model-artifact"
            if relative == "models/artifacts/README.md":
                return "model-artifact-documentation"
            if relative.startswith("profiles/") and relative.lower().endswith(".json"):
                return "retrieval-profile"
            if relative.startswith("schemas/") and relative.lower().endswith(".json"):
                return "json-schema"
            self.fail(f"unclassified expected runtime asset {relative}")

        expected_shared_assets: list[tuple[str, str]] = []
        for relative_root in (
            "evals/conformance",
            "models",
            "profiles",
            "schemas",
        ):
            for asset in (knowledge_root / relative_root).rglob("*"):
                if asset.is_file():
                    relative = asset.relative_to(knowledge_root).as_posix()
                    expected_shared_assets.append(
                        (shared_asset_kind(relative), relative)
                    )
        expected_shared_assets.sort(key=lambda row: row[1])
        self.assertEqual(
            [(row["kind"], row["path"]) for row in shared_assets],
            expected_shared_assets,
        )
        for row in shared_assets:
            self.assertEqual(
                set(row), {"kind", "path", "sha256", "size", "mode"}
            )
            self.assertEqual(row["mode"], "0644")
            asset = knowledge_root / row["path"]
            self.assertTrue(asset.is_file(), f"missing shared asset {row['path']}")
            digest = sha256(asset)
            self.assertEqual(row["size"], asset.stat().st_size)
            self.assertEqual(row["sha256"], "sha256:" + digest)
            expected_sums["../" + row["path"]] = digest

        notices = manifest["notices"]
        self.assertEqual(notices["kind"], "third-party-notices")
        self.assertEqual(notices["path"], "THIRD_PARTY_NOTICES.md")
        self.assertEqual(notices["mode"], "0644")
        notices_path = BIN_ROOT / notices["path"]
        self.assertTrue(notices_path.is_file(), "missing packaged notices")
        notices_digest = sha256(notices_path)
        self.assertEqual(notices["sha256"], "sha256:" + notices_digest)
        self.assertEqual(notices["size"], notices_path.stat().st_size)
        expected_sums[notices["path"]] = notices_digest
        expected_bin_files.add(notices["path"])

        expected_sums["manifest.json"] = sha256(manifest_path)
        self.assertEqual(sums, expected_sums)
        actual_bin_files = {
            path.relative_to(BIN_ROOT).as_posix()
            for path in BIN_ROOT.rglob("*")
            if path.is_file()
        }
        self.assertEqual(actual_bin_files, expected_bin_files)

    def make_project(self, root: Path) -> None:
        profile = (PLUGIN / "templates" / "project" / "project-profile.md").read_text(
            encoding="utf-8"
        )
        profile = (
            profile.replace("{{PROJECT_NAME}}", "runtime-integration")
            .replace("{{PROJECT_TYPE}}", "test")
            .replace("{{ONE_LINE_FRAMING}}", "dual-host packaged runtime fixture")
            .replace("{{MISSION}}", "Verify portable shared knowledge.")
            .replace("{{DOMAIN_DESCRIPTION}}", "Packaged runtime integration.")
            .replace(
                "{{SOURCE_OF_RECORD}}",
                "docs/truth/findings/runtime-integration/F-0101.md",
            )
            .replace("{{TOOLING}}", "Packaged MCP server.")
            .replace("{{BINARIES_AND_PATHS}}", "No machine-local paths.")
            .replace("{{ENVIRONMENT}}", "Isolated test process.")
            .replace("{{WALL_EXAMPLE}}", "The truth fixture is direct evidence.")
        )
        profile_path = root / ".re-discipline" / "project-profile.md"
        profile_path.parent.mkdir(parents=True)
        profile_path.write_text(profile, encoding="utf-8")
        truth = root / "docs" / "truth"
        truth.mkdir(parents=True)
        (root / "docs" / "INDEX.md").write_text(
            "# Documentation Index\n\n- [Truth](truth/INDEX.md)\n",
            encoding="utf-8",
        )
        (truth / "INDEX.md").write_text(
            "# Truth Index\n\n"
            "- [Runtime](findings/runtime-integration/F-0101.md)\n",
            encoding="utf-8",
        )
        finding = truth / "findings" / "runtime-integration" / "F-0101.md"
        finding.parent.mkdir(parents=True)
        shutil.copyfile(
            PLUGIN
            / "knowledge"
            / "evals"
            / "conformance"
            / "fixture"
            / "docs"
            / "truth"
            / "findings"
            / "F-0101.md",
            finding,
        )

    def launch_environment(self, temporary: Path) -> tuple[dict[str, str], Path, Path]:
        claude_home = temporary / "native" / "claude"
        codex_home = temporary / "native" / "codex"
        claude_memory = claude_home / "projects" / "fixture" / "memory"
        codex_memory = codex_home / "memories"
        claude_memory.mkdir(parents=True)
        codex_memory.mkdir(parents=True)
        (claude_memory / "MEMORY.md").write_text(
            "CLAUDE_NATIVE_MEMORY_SENTINEL", encoding="utf-8"
        )
        (codex_memory / "MEMORY.md").write_text(
            "CODEX_NATIVE_MEMORY_SENTINEL", encoding="utf-8"
        )
        environment = os.environ.copy()
        environment.update(
            {
                "CLAUDE_CONFIG_DIR": str(claude_home),
                "CODEX_HOME": str(codex_home),
                "HOME": str(temporary / "isolated-home"),
                "USERPROFILE": str(temporary / "isolated-profile"),
                "APPDATA": str(temporary / "app-data"),
                "LOCALAPPDATA": str(temporary / "local-app-data"),
                "XDG_CACHE_HOME": str(temporary / "xdg-cache"),
                "CLAUDE_PROJECT_DIR": "",
            }
        )
        return environment, claude_home, codex_home

    def extract_declarations(self, plugin_root: Path) -> tuple[dict, dict]:
        claude_manifest = json.loads(
            (plugin_root / ".claude-plugin" / "plugin.json").read_text(encoding="utf-8")
        )
        self.assertEqual(claude_manifest.get("mcpServers"), "./.mcp.json")
        claude_servers = json.loads(
            (plugin_root / ".mcp.json").read_text(encoding="utf-8")
        )
        self.assertNotIn("mcpServers", claude_servers)

        codex_manifest = json.loads(
            (plugin_root / ".codex-plugin" / "plugin.json").read_text(encoding="utf-8")
        )
        codex_servers = codex_manifest.get("mcpServers")
        self.assertIsInstance(
            codex_servers,
            dict,
            "Codex MCP config must be inline because its companion schema conflicts with Claude",
        )
        return (
            claude_servers["re-discipline-knowledge"],
            codex_servers["re-discipline-knowledge"],
        )

    def run_preflight(
        self,
        plugin_root: Path,
        declaration: dict,
        project: Path,
        environment: dict[str, str],
    ) -> dict:
        executable = command_path(plugin_root, declaration["command"])
        self.assertTrue(executable.is_file(), executable)
        completed = subprocess.run(
            [
                str(executable),
                "preflight",
                "--asset-root",
                str(plugin_root / "knowledge"),
                "--project-root",
                str(project),
            ],
            cwd=plugin_root,
            env=environment,
            text=True,
            capture_output=True,
            timeout=60,
            check=False,
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        return json.loads(completed.stdout)

    def run_mcp(
        self,
        plugin_root: Path,
        declaration: dict,
        project: Path,
        environment: dict[str, str],
    ) -> dict[int, dict]:
        executable = command_path(plugin_root, declaration["command"])
        self.assertTrue(executable.is_file(), executable)
        self.assertEqual(executable.parent, plugin_root / "knowledge" / "bin")
        self.assertNotIn("python", executable.name.lower())
        self.assertNotIn("node", executable.name.lower())
        self.assertNotIn("go", executable.name.lower())
        arguments = [
            substitute_plugin_root(str(argument), plugin_root)
            for argument in declaration.get("args", [])
        ]
        requests = [
            {
                "jsonrpc": "2.0",
                "id": 1,
                "method": "initialize",
                "params": {
                    "protocolVersion": "2025-06-18",
                    "capabilities": {},
                    "clientInfo": {"name": "packaged-runtime-test", "version": "1"},
                },
            },
            {"jsonrpc": "2.0", "method": "notifications/initialized"},
            {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}},
            {
                "jsonrpc": "2.0",
                "id": 3,
                "method": "tools/call",
                "params": {
                    "name": "state",
                    "arguments": {
                        "projectRoot": str(project),
                        "mode": "orient",
                    },
                },
            },
            {
                "jsonrpc": "2.0",
                "id": 4,
                "method": "tools/call",
                "params": {
                    "name": "read",
                    "arguments": {
                        "projectRoot": str(project),
                        "selector": "path",
                        "value": "docs/truth/findings/runtime-integration/F-0101.md",
                        "tokenBudget": 512,
                    },
                },
            },
        ]
        payload = "".join(json.dumps(request) + "\n" for request in requests)
        cwd_value = declaration.get("cwd", ".")
        cwd = Path(substitute_plugin_root(str(cwd_value), plugin_root))
        if not cwd.is_absolute():
            cwd = plugin_root / cwd
        completed = subprocess.run(
            [str(executable), *arguments],
            cwd=cwd,
            env=environment,
            input=payload,
            text=True,
            capture_output=True,
            timeout=60,
            check=False,
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        responses = [json.loads(line) for line in completed.stdout.splitlines() if line.strip()]
        return {
            int(response["id"]): response
            for response in responses
            if "id" in response and isinstance(response["id"], (int, float))
        }

    def assert_mcp_result(self, responses: dict[int, dict]) -> tuple[str, str]:
        self.assertEqual(
            responses[1]["result"]["serverInfo"]["version"],
            DECLARED_VERSION,
        )
        self.assertEqual(
            [tool["name"] for tool in responses[2]["result"]["tools"]],
            EXPECTED_TOOLS,
        )
        state = responses[3]["result"]
        self.assertFalse(state["isError"], state)
        state_body = state["structuredContent"]
        self.assertEqual(state_body["mode"], "orient")
        self.assertRegex(state_body["digest"], r"^sha256:[0-9a-f]{64}$")
        read = responses[4]["result"]
        self.assertFalse(read["isError"], read)
        read_body = read["structuredContent"]
        self.assertEqual(
            read_body["path"],
            "docs/truth/findings/runtime-integration/F-0101.md",
        )
        self.assertIn("celadonquartzaurora", read_body["content"])
        self.assertRegex(read_body["sha256"], r"^sha256:[0-9a-f]{64}$")
        self.assertRegex(read_body["digest"], r"^sha256:[0-9a-f]{64}$")
        return state_body["digest"], read_body["sha256"]

    def test_relocated_codex_and_claude_launch_same_shared_project(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            claude_copy = temporary / "claude-cache" / "re-discipline" / DECLARED_VERSION
            codex_copy = temporary / "codex-cache" / "re-discipline" / DECLARED_VERSION
            shutil.copytree(PLUGIN, claude_copy, copy_function=copy_or_link)
            shutil.copytree(PLUGIN, codex_copy, copy_function=copy_or_link)
            project = temporary / "project"
            self.make_project(project)
            environment, claude_home, codex_home = self.launch_environment(temporary)
            native_before = {
                "claude": snapshot_tree(claude_home),
                "codex": snapshot_tree(codex_home),
            }
            claude_declaration, _ = self.extract_declarations(claude_copy)
            _, codex_declaration = self.extract_declarations(codex_copy)

            preflight = self.run_preflight(
                codex_copy, codex_declaration, project, environment
            )
            self.assertEqual(
                preflight["system"]["configuration"]["memoryMode"],
                "shared-only",
            )
            self.assertTrue((project / ".re-discipline" / "config.json").is_file())
            self.assertTrue((project / ".claude" / "settings.json").is_file())
            self.assertTrue((project / ".codex" / "config.toml").is_file())
            self.assertFalse(
                json.loads(
                    (project / ".claude" / "settings.json").read_text(encoding="utf-8")
                )["autoMemoryEnabled"]
            )
            codex_policy = (project / ".codex" / "config.toml").read_text(
                encoding="utf-8"
            )
            for policy in (
                "memories = false",
                "generate_memories = false",
                "use_memories = false",
            ):
                self.assertIn(policy, codex_policy)

            codex_result = self.assert_mcp_result(
                self.run_mcp(codex_copy, codex_declaration, project, environment)
            )
            claude_result = self.assert_mcp_result(
                self.run_mcp(claude_copy, claude_declaration, project, environment)
            )
            self.assertEqual(codex_result, claude_result)
            self.assertEqual(snapshot_tree(claude_home), native_before["claude"])
            self.assertEqual(snapshot_tree(codex_home), native_before["codex"])

    def test_packaged_cli_status_preserves_malformed_configuration_and_fails_closed(
        self,
    ) -> None:
        claude_declaration, _ = self.extract_declarations(PLUGIN)
        executable = command_path(PLUGIN, claude_declaration["command"])
        for relative, malformed in (
            (".re-discipline/config.json", b'{"schemaVersion":'),
            (
                ".re-discipline/knowledge/policy.jsonc",
                b'{"schemaVersion":1,"sources":',
            ),
        ):
            with self.subTest(relative=relative), tempfile.TemporaryDirectory() as directory:
                temporary = Path(directory)
                project = temporary / "project"
                self.make_project(project)
                environment, _, _ = self.launch_environment(temporary)
                self.run_preflight(
                    PLUGIN, claude_declaration, project, environment
                )
                control_path = project / relative
                control_path.write_bytes(malformed)
                cache_root = project / ".re-discipline" / "cache" / "knowledge"
                cache_before = snapshot_tree(cache_root)

                status = subprocess.run(
                    [
                        str(executable),
                        "status",
                        "--asset-root",
                        str(PLUGIN / "knowledge"),
                        "--project-root",
                        str(project),
                    ],
                    cwd=PLUGIN,
                    env=environment,
                    text=True,
                    capture_output=True,
                    timeout=60,
                    check=False,
                )
                self.assertEqual(status.returncode, 0, status.stderr)
                status_body = json.loads(status.stdout)
                self.assertFalse(status_body["system"]["configuration"]["valid"], status_body)
                self.assertTrue(status_body["system"]["configuration"]["errors"], status_body)
                self.assertFalse(
                    status_body["system"]["index"]["mutationPerformed"], status_body
                )
                self.assertEqual(control_path.read_bytes(), malformed)
                self.assertEqual(snapshot_tree(cache_root), cache_before)

                for command, standard_input in (
                    (["index"], None),
                    (
                        ["context-pack-materialize", "--input", "-"],
                        json.dumps(
                            {
                                "action": "preview",
                                "target": {
                                    "kind": "recruiting-run",
                                    "candidateSlug": "fixture-candidate",
                                    "recruitingRunId": "20260802T190000Z",
                                },
                                "task": "engine checksum",
                                "role": "drafter",
                                "allowedTiers": ["truth"],
                                "tokenBudget": 1024,
                            }
                        ),
                    ),
                ):
                    failed = subprocess.run(
                        [
                            str(executable),
                            *command,
                            "--asset-root",
                            str(PLUGIN / "knowledge"),
                            "--project-root",
                            str(project),
                        ],
                        cwd=PLUGIN,
                        env=environment,
                        input=standard_input,
                        text=True,
                        capture_output=True,
                        timeout=60,
                        check=False,
                    )
                    self.assertNotEqual(
                        failed.returncode,
                        0,
                        f"{command[0]} did not fail closed: {failed.stdout}",
                    )
                    self.assertIn("configuration", failed.stderr.lower())
                    self.assertEqual(control_path.read_bytes(), malformed)
                    self.assertEqual(snapshot_tree(cache_root), cache_before)

    def test_unknown_cli_command_prints_usage_without_recovery(self) -> None:
        claude_declaration, _ = self.extract_declarations(PLUGIN)
        executable = command_path(PLUGIN, claude_declaration["command"])
        self.assertTrue(executable.is_file(), executable)
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            project = temporary / "project"
            self.make_project(project)
            environment, _, _ = self.launch_environment(temporary)
            config_path = project / ".re-discipline" / "config.json"
            config_path.unlink(missing_ok=True)
            before = snapshot_tree(project)

            completed = subprocess.run(
                [
                    str(executable),
                    "sttaus",
                    "--asset-root",
                    str(PLUGIN / "knowledge"),
                    "--project-root",
                    str(project),
                ],
                cwd=project,
                env=environment,
                text=True,
                capture_output=True,
                timeout=60,
                check=False,
            )

            after = snapshot_tree(project)
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("usage: re-discipline-knowledge", completed.stderr)
        self.assertEqual(after, before)
        self.assertNotIn(".re-discipline/config.json", after)


if __name__ == "__main__":
    unittest.main()
