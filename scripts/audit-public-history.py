#!/usr/bin/env python3
import re
import subprocess
import sys

PROHIBITED_PATHS = (
    re.compile(r"(^|/)HANDOVER\.md$"),
    re.compile(r"(^|/)\.idea/"),
)
PROHIBITED_CONTENT = (
    re.compile("/" + "Users/"),
    re.compile("packetstream" + r"\.dev"),
    re.compile("localhost:" + "26423"),
    re.compile("AKIAIOSFODNN7" + "EXAMPLE"),
    re.compile("wJalrXUtnFEMI/K7MDENG/bPxRfiCY" + "EXAMPLEKEY"),
)
API_KEY_PATTERN = re.compile(r"sk_nc_[A-Za-z0-9…_-]{8,}")
ALLOWED_API_KEYS = {"sk_nc_test", "sk_nc_env", "sk_nc_explicit"}


def git_output(*arguments):
    return subprocess.check_output(["git", *arguments])


def main():
    if git_output("rev-parse", "--is-shallow-repository").strip() == b"true":
        raise SystemExit("full history is required; fetch with depth 0")

    revisions = git_output("rev-list", "--all").decode().splitlines()
    findings = []
    scanned_blobs = set()

    for revision in revisions:
        entries = git_output("ls-tree", "-r", "-z", revision).split(b"\0")
        for entry in filter(None, entries):
            metadata, raw_path = entry.split(b"\t", 1)
            object_id = metadata.split()[2].decode()
            file_path = raw_path.decode(errors="replace")
            if any(pattern.search(file_path) for pattern in PROHIBITED_PATHS):
                findings.append(f"{revision}:{file_path}: prohibited historical path")
            if object_id in scanned_blobs:
                continue
            scanned_blobs.add(object_id)
            content = git_output("cat-file", "blob", object_id).decode(errors="ignore")
            for pattern in PROHIBITED_CONTENT:
                if pattern.search(content):
                    findings.append(f"{revision}:{file_path}: prohibited content")
            for api_key in API_KEY_PATTERN.findall(content):
                if api_key not in ALLOWED_API_KEYS and "..." not in api_key:
                    findings.append(f"{revision}:{file_path}: credential-shaped API key")

    if findings:
        print("\n".join(sorted(set(findings))), file=sys.stderr)
        raise SystemExit(1)
    print(f"public-history audit passed ({len(revisions)} commits, {len(scanned_blobs)} blobs)")


if __name__ == "__main__":
    main()
