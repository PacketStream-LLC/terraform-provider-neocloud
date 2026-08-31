#!/usr/bin/env bash
# neocloud-api 의 공개 스펙을 vendor 하고 Go 클라이언트를 재생성한다.
set -euo pipefail
cd "$(dirname "$0")/.."

SRC="${NEOCLOUD_API_REPO:-../neocloud-api}/openapi-specs/neocloud-public.json"
cp "$SRC" openapi/neocloud-public.json

python3 scripts/downconvert-3.0.py openapi/neocloud-public.json openapi/generator-input.json

if grep -q '"null"' openapi/generator-input.json; then
    echo 'downconvert 실패: 3.1 null 타입이 남아 있다' >&2
    grep -n '"null"' openapi/generator-input.json | head >&2
    exit 1
fi

go tool oapi-codegen -config oapi-codegen.yaml openapi/generator-input.json
gofmt -w internal/client/gen.go
echo "regenerated internal/client/gen.go"
