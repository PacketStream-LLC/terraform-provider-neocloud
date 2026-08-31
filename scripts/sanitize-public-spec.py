#!/usr/bin/env python3
import json
import sys

PRODUCTION_API_URL = "https://neocloud-api.packetstream.us"
SAFE_EXAMPLES = {
    "AKIAIOSFODNN7" + "EXAMPLE": "EXAMPLEACCESSKEY",
    "wJalrXUtnFEMI/K7MDENG/bPxRfiCY" + "EXAMPLEKEY": "EXAMPLESECRETKEY",
}


def replace_credential_examples(value):
    if isinstance(value, list):
        return [replace_credential_examples(item) for item in value]
    if isinstance(value, dict):
        return {
            key: replace_credential_examples(item)
            for key, item in value.items()
        }
    if isinstance(value, str):
        return SAFE_EXAMPLES.get(value, value)
    return value


def main():
    spec_path = sys.argv[1]
    with open(spec_path) as spec_file:
        spec = json.load(spec_file)

    spec = replace_credential_examples(spec)
    spec["servers"] = [
        server
        for server in spec.get("servers", [])
        if server.get("url") == PRODUCTION_API_URL
    ]

    with open(spec_path, "w") as spec_file:
        json.dump(spec, spec_file, ensure_ascii=False, indent=2)
        spec_file.write("\n")


if __name__ == "__main__":
    main()
