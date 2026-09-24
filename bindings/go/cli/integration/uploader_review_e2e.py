#!/usr/bin/env python3
"""Local-only uploader review diagnostics; Python standard library, Go cache required.

Run from any directory:
  python3 bindings/go/cli/integration/uploader_review_e2e.py
  python3 bindings/go/cli/integration/uploader_review_e2e.py --case default --case put

All artifacts are retained. Statuses describe observations, not a regression-test
contract: confirmed = case-specific evidence, not_reproduced = evidence absent,
observed = policy/timing-dependent outcome, harness_error = setup/execution failure.
Only harness errors produce exit code 1. No Docker, plugins, or remote resources
are used. GOPROXY=off and GOTOOLCHAIN=local require preinstalled Go dependencies.
"""

import argparse
import hashlib
import json
import os
import signal
import subprocess
import tempfile
import threading
import time
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

CASES = (
    "default",
    "put",
    "extra-identity",
    "nested-literal",
    "macro-shadow",
    "triple-quotes",
    "redirect",
    "query-token",
    "oci",
    "early-response",
)
PAYLOAD = b"original resource bytes from local CLI review fixture\n"
TOKEN = "synthetic-review-query-secret"
IMAGE_CONFIG = (
    b'{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}'
)
MANIFEST_TYPE = "application/vnd.oci.image.manifest.v1+json"
CONFIG_TYPE = "application/vnd.oci.image.config.v1+json"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


class Fixture(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False

    def __init__(self, folder):
        super().__init__(("127.0.0.1", 0), Handler)
        self.folder = folder
        self.lock = threading.Lock()
        self.events = []
        self.errors = []
        self.stored = {}
        self.manifest = json.dumps(
            {
                "schemaVersion": 2,
                "mediaType": MANIFEST_TYPE,
                "config": {
                    "mediaType": CONFIG_TYPE,
                    "digest": "sha256:" + sha(IMAGE_CONFIG),
                    "size": len(IMAGE_CONFIG),
                },
                "layers": [],
            },
            separators=(",", ":"),
        ).encode()
        self.base = "http://127.0.0.1:" + str(self.server_port)

    def handle_error(self, request, client_address):
        with self.lock:
            self.errors.append(traceback.format_exc())

    def snapshot(self):
        with self.lock:
            return list(self.events)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def setup(self):
        super().setup()
        self.connection.settimeout(15)

    def body(self):
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            chunks = []
            while True:
                size = int(self.rfile.readline().split(b";", 1)[0], 16)
                if not size:
                    while self.rfile.readline() not in (b"\r\n", b"\n", b""):
                        pass
                    return b"".join(chunks)
                chunk = self.rfile.read(size)
                if len(chunk) != size or self.rfile.read(2) != b"\r\n":
                    raise ValueError("incomplete chunked request")
                chunks.append(chunk)
        size = int(self.headers.get("Content-Length", "0"))
        data = self.rfile.read(size)
        if len(data) != size:
            raise ValueError("incomplete request body")
        return data

    def reply(self, body=b"", status=200, headers=None):
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def serve(self):
        path = urlsplit(self.path).path
        early = path.endswith("/early-response") and self.command == "PUT"
        body = b"" if early else self.body()
        event = {
            "method": self.command,
            "path": self.path,
            "headers": dict(self.headers),
            "body_size": len(body),
            "body_sha256": sha(body),
            "body_consumed": not early,
        }
        with self.server.lock:
            self.server.events.append(event)
        if path.startswith("/source/"):
            data = (
                PAYLOAD if not path.endswith("/early-response") else b"x" * (32 << 20)
            )
            self.reply(data)
        elif path.startswith("/v2/"):
            if self.command not in ("GET", "HEAD"):
                self.reply(status=405)
                return
            manifest = self.server.manifest
            if path == "/v2/":
                self.reply()
            elif path in (
                "/v2/review/image/manifests/latest",
                "/v2/review/image/manifests/sha256:" + sha(manifest),
            ):
                self.reply(
                    manifest,
                    headers={
                        "Content-Type": MANIFEST_TYPE,
                        "Docker-Content-Digest": "sha256:" + sha(manifest),
                    },
                )
            elif path == "/v2/review/image/blobs/sha256:" + sha(IMAGE_CONFIG):
                self.reply(
                    IMAGE_CONFIG,
                    headers={
                        "Content-Type": CONFIG_TYPE,
                        "Docker-Content-Digest": "sha256:" + sha(IMAGE_CONFIG),
                    },
                )
            else:
                self.reply(status=404)
        elif path == "/target/query-token":
            self.connection.sendall(b"not an HTTP response\r\n\r\n")
            self.close_connection = True
        elif early:
            self.reply(headers={"Connection": "close"})
            self.close_connection = True
        elif path == "/target/redirect":
            self.reply(status=302, headers={"Location": "/page"})
        elif path == "/page":
            self.reply(b"ordinary GET page; nothing was stored")
        elif path.startswith("/target/"):
            if self.command == "PUT":
                with self.server.lock:
                    self.server.stored[path] = body
                artifact = self.server.folder / (
                    path.rsplit("/", 1)[-1] + "-uploaded.bin"
                )
                artifact.write_bytes(body)
                self.reply()
            elif self.command == "GET":
                with self.server.lock:
                    data = self.server.stored.get(path, b"")
                self.reply(data)
            else:
                self.reply(status=405)
        else:
            self.reply(status=404)

    def do_GET(self):
        try:
            self.serve()
        except (BrokenPipeError, ConnectionResetError):
            # Normal for early responses and CLI cancellation.
            pass

    do_HEAD = do_GET
    do_PUT = do_GET
    do_POST = do_GET


class HarnessError(RuntimeError):
    pass


class Runner:
    def __init__(self, output, timeout):
        self.output = output
        self.timeout = timeout
        self.cwd = Path(__file__).resolve().parents[1]
        self.commands = []
        self.env = dict(os.environ)
        for key in list(self.env):
            if key.upper().endswith("_PROXY") or key.startswith("OCM_"):
                del self.env[key]
        self.env.update(
            GOPROXY="off",
            GOSUMDB="off",
            GOTOOLCHAIN="local",
            NO_PROXY="127.0.0.1,localhost",
        )
        self.plugins = output / "plugins"
        self.plugins.mkdir()

    def run(self, folder, label, config, args):
        temp = folder / (label + "-temp")
        temp.mkdir()
        argv = [
            "go",
            "run",
            "main.go",
            "--config",
            str(config),
            "--temp-folder",
            str(temp),
            "--plugin-directory",
            str(self.plugins),
            "--working-directory",
            str(folder),
        ] + args
        record = {
            "label": label,
            "argv": argv,
            "cwd": str(self.cwd),
            "stdout": str(folder / (label + ".stdout")),
            "stderr": str(folder / (label + ".stderr")),
        }
        self.commands.append(record)
        write_json(folder / (label + "-command.json"), record)
        started = time.monotonic()
        with (
            open(record["stdout"], "wb") as stdout,
            open(record["stderr"], "wb") as stderr,
        ):
            proc = subprocess.Popen(
                argv,
                cwd=self.cwd,
                env=self.env,
                stdout=stdout,
                stderr=stderr,
                start_new_session=True,
            )
            try:
                record["returncode"] = proc.wait(timeout=self.timeout)
            except BaseException:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait()
                record["harness_error"] = "subprocess interrupted or timed out"
                raise
            finally:
                record["seconds"] = round(time.monotonic() - started, 3)
                record["temp_after_exit"] = [
                    str(p.relative_to(temp)) for p in temp.rglob("*")
                ]
                record["temp_observation"] = (
                    "entries remain after exit"
                    if record["temp_after_exit"]
                    else "empty after exit"
                )
                write_json(folder / (label + "-command.json"), record)
        text = Path(record["stderr"]).read_text(errors="replace")
        if any(
            marker in text
            for marker in (
                "module lookup disabled",
                "requires go >=",
                "unknown flag:",
                "command not found",
                "no required module provides",
            )
        ):
            raise HarnessError(text)
        return record, text

    def case(self, name, server):
        folder = self.output / name
        folder.mkdir()
        start = len(server.snapshot())
        result = {"case": name, "status": "observed"}
        try:
            self.execute_case(name, server, folder, result)
        except Exception as exc:
            result.update(
                status="harness_error", error=str(exc), traceback=traceback.format_exc()
            )
        finally:
            result["requests"] = server.snapshot()[start:]
            write_json(folder / "result.json", result)
        return result

    def execute_case(self, name, server, folder, result):
        source = folder / "source-ctf"
        target = folder / "target-ctf"
        component = "ocm.software/uploader-review"
        suffix = "//" + component + ":1.0.0"
        access = {"type": "wget/v1", "url": server.base + "/source/" + name}
        if name == "oci":
            access = {
                "type": "ociArtifact/v1",
                "imageReference": server.base + "/review/image:latest",
            }
        resource = {
            "name": "blob",
            "version": "1.0.0",
            "type": "ociArtifact" if name == "oci" else "blob",
            "relation": "external",
            "access": access,
        }
        resources = [resource]
        if name == "extra-identity":
            resources.insert(0, dict(resource, name="plain"))
            resource["extraIdentity"] = {"tier": "public"}
            result["fixture_shape"] = (
                "plain resource without extraIdentity before selected resource"
            )
        constructor = folder / "constructor.json"
        write_json(
            constructor,
            {
                "components": [
                    {
                        "name": component,
                        "version": "1.0.0",
                        "provider": {"name": "ocm.software"},
                        "resources": resources,
                    }
                ]
            },
        )
        config = folder / "base-config.json"
        write_json(
            config, {"type": "generic.config.ocm.software/v1", "configurations": []}
        )
        add, error = self.run(
            folder,
            "add",
            config,
            [
                "add",
                "cv",
                "--repository",
                "ctf::" + str(source),
                "--constructor",
                str(constructor),
            ],
        )
        if add["returncode"]:
            raise HarnessError("source constructor failed: " + error)
        target_url = server.base + "/target/" + name
        if name == "query-token":
            target_url += "?token=" + TOKEN
        uploader = {
            "type": "http.uploader.transfer.config.ocm.software/v1alpha1",
            "match": {"accessType": access["type"]},
            "targetURL": "${" + json.dumps(target_url) + "}",
        }
        if name != "default":
            uploader["method"] = "PUT"
        if name == "extra-identity":
            uploader["match"]["extraIdentity"] = resource["extraIdentity"]
            uploader["targetURL"] = (
                "${" + json.dumps(target_url + "/") + " + resource.name}"
            )
            result["expected_target_path"] = "/target/extra-identity/blob"
        expressions = {
            "nested-literal": (
                '${resource.name}/${"${resource.name}"}',
                "blob/${resource.name}",
            ),
            "macro-shadow": (
                '${["x"].map(resource, resource)[0] + resource.name}',
                "xblob",
            ),
            "triple-quotes": (
                '${"""a " resource.name " b""" + resource.name}',
                'a " resource.name " bblob',
            ),
        }
        if name in expressions:
            uploader["header"] = {"X-Review-Template": [expressions[name][0]]}
            result["expected_header"] = expressions[name][1]
        transfer_config = folder / "transfer-config.json"
        write_json(
            transfer_config,
            {"type": "generic.config.ocm.software/v1", "configurations": [uploader]},
        )
        before = len(server.snapshot())
        transfer, error = self.run(
            folder,
            "transfer",
            transfer_config,
            ["transfer", "cv", "ctf::" + str(source) + suffix, "ctf::" + str(target)],
        )
        events = server.snapshot()[before:]
        uploads = [e for e in events if e["path"].startswith("/target/")]
        result.update(
            transfer_returncode=transfer["returncode"], upload_requests=uploads
        )
        if name in ("default", "put"):
            if (
                transfer["returncode"]
                or not uploads
                or uploads[0]["body_sha256"] != sha(PAYLOAD)
            ):
                raise HarnessError("readback prerequisite failed: " + error)
            downloaded = folder / "download.bin"
            before = len(server.snapshot())
            download, error = self.run(
                folder,
                "download",
                config,
                [
                    "download",
                    "resource",
                    "ctf::" + str(target) + suffix,
                    "--identity",
                    "name=blob,version=1.0.0",
                    "--output",
                    str(downloaded),
                ],
            )
            read_events = server.snapshot()[before:]
            result.update(
                download_returncode=download["returncode"],
                readback_requests=read_events,
                downloaded_matches=downloaded.is_file()
                and downloaded.read_bytes() == PAYLOAD,
            )
            with server.lock:
                result["stored_size_after_readback"] = len(
                    server.stored.get("/target/" + name, b"")
                )
            if name == "default":
                ok = (
                    not download["returncode"]
                    and result["downloaded_matches"]
                    and any(e["method"] == "GET" for e in read_events)
                )
                result.update(
                    status="confirmed" if ok else "harness_error",
                    finding="default-method readback control",
                )
            else:
                overwritten = (
                    any(
                        e["method"] == "PUT" and e["body_size"] == 0
                        for e in read_events
                    )
                    and result["stored_size_after_readback"] == 0
                )
                if download["returncode"] and not overwritten:
                    raise HarnessError("unexpected readback failure: " + error)
                result.update(
                    status="confirmed" if overwritten else "not_reproduced",
                    finding="explicit PUT reused on download and overwrote object",
                )
        elif name == "extra-identity":
            hit = (
                transfer["returncode"] != 0
                and "extraIdentity" in error
                and any(s in error.lower() for s in ("overload", "check", "compile"))
            )
            if transfer["returncode"] and not hit:
                raise HarnessError(
                    "unexpected extra-identity transfer failure: " + error
                )
            if not transfer["returncode"] and not any(
                e["path"] == result["expected_target_path"]
                and e["method"] == "PUT"
                and e["body_sha256"] == sha(PAYLOAD)
                for e in uploads
            ):
                raise HarnessError(
                    "resource.name target URL did not receive the expected upload"
                )
            result.update(
                status="confirmed" if hit else "not_reproduced",
                finding="extraIdentity graph checking failure",
            )
        elif name in expressions:
            actual = [e["headers"].get("X-Review-Template") for e in uploads]
            result["actual_headers"] = actual
            changed = bool(actual) and any(v != expressions[name][1] for v in actual)
            compile_failure = transfer["returncode"] != 0 and any(
                s in error.lower()
                for s in (
                    "undeclared",
                    "overload",
                    "syntax error",
                    "argument is not an identifier",
                )
            )
            if transfer["returncode"] and not compile_failure:
                raise HarnessError("unexpected template transfer failure: " + error)
            if not transfer["returncode"] and not uploads:
                raise HarnessError("template target was never reached")
            result.update(
                status="confirmed" if changed or compile_failure else "not_reproduced",
                finding="CEL template changed or rejected",
                compile_failure=compile_failure,
            )
        elif name == "query-token":
            if transfer["returncode"] and "malformed HTTP" not in error:
                raise HarnessError("unexpected query-token transfer failure: " + error)
            hit = (
                bool(uploads)
                and transfer["returncode"] != 0
                and any(
                    TOKEN in line and "malformed HTTP" in line
                    for line in error.splitlines()
                )
            )
            result.update(
                status="confirmed" if hit else "not_reproduced",
                finding="synthetic query token in transport-error stderr",
            )
        elif name == "oci":
            result["manifest_sha256"] = sha(server.manifest)
            result["uploaded_sha256"] = uploads[0]["body_sha256"] if uploads else None
            mismatch = (
                bool(uploads)
                and uploads[0]["body_size"] > 0
                and uploads[0]["body_sha256"] != sha(server.manifest)
            )
            hit = (
                mismatch
                and transfer["returncode"] != 0
                and "digest" in error.lower()
                and "mismatch" in error.lower()
            )
            if transfer["returncode"] and not hit:
                raise HarnessError("unexpected OCI transfer failure: " + error)
            result.update(
                status="confirmed" if hit else "not_reproduced",
                finding="OCI manifest digest compared with materialized archive digest",
            )
        elif name == "redirect":
            result.update(
                finding="redirect policy observation; no mandatory error asserted",
                consumed_put=any(
                    e["method"] == "PUT" and e["body_sha256"] == sha(PAYLOAD)
                    for e in uploads
                ),
                followed_get=any(
                    e["method"] == "GET" and e["path"] == "/page" for e in events
                ),
            )
        else:
            result.update(
                finding="early 200 with normal 32 MiB wget source; descriptor digest may reject incomplete upload",
                early_target_reached=bool(uploads),
                rejected_digest_mismatch=transfer["returncode"] != 0
                and "digest mismatch" in error,
                source_size=32 << 20,
            )
        if (
            name
            not in ("extra-identity", "nested-literal", "macro-shadow", "triple-quotes")
            and not uploads
        ):
            raise HarnessError("target was never reached: " + error)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--case", action="append", choices=CASES, help="repeat to select cases"
    )
    parser.add_argument(
        "--output-dir", type=Path, help="new directory (must not already exist)"
    )
    parser.add_argument(
        "--timeout", type=float, default=45, help="seconds per go run (default: 45)"
    )
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    if args.output_dir:
        output = args.output_dir.resolve()
        output.mkdir(parents=True, exist_ok=False)
    else:
        output = Path(tempfile.mkdtemp(prefix="ocm-uploader-review-"))
    print("Artifacts: " + str(output), flush=True)
    report = {
        "cases": [],
        "commands": [],
        "notes": [
            "Only loopback resource endpoints; preinstalled Go toolchain/module cache required.",
            "Temp entries are post-process observations, not proof of permanent leaks.",
            "Request logs contain only a synthetic token; artifacts deliberately retained.",
        ],
    }
    server = None
    thread = None
    try:
        runner = Runner(output, args.timeout)
        report["commands"] = runner.commands
        server = Fixture(output)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        report["fixture_url"] = server.base
        for name in dict.fromkeys(args.case or CASES):
            result = runner.case(name, server)
            report["cases"].append(result)
            print(name + ": " + result["status"], flush=True)
            write_json(output / "report.json", report)
    except (Exception, KeyboardInterrupt) as exc:
        report["harness_error"] = str(exc) or type(exc).__name__
        report["traceback"] = traceback.format_exc()
    finally:
        if server:
            server.shutdown()
            server.server_close()
            if thread:
                thread.join(timeout=5)
            report["fixture_errors"] = server.errors
            write_json(output / "requests.json", server.snapshot())
        write_json(output / "report.json", report)
    return int(
        bool(
            report.get("harness_error")
            or report.get("fixture_errors")
            or any(r["status"] == "harness_error" for r in report["cases"])
        )
    )


if __name__ == "__main__":
    raise SystemExit(main())
