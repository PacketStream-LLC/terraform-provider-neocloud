#!/usr/bin/env python3
"""oapi-codegen(kin-openapi) 은 OpenAPI 3.0 만 받는다 — 생성기 입력만 3.0 으로 낮춘다.

vendor 스펙(openapi/neocloud-public.json)은 3.1 원본 그대로 두고, 이 산출물은
커밋하지 않는다. 규칙은 neocloud 스펙에 실재하는 nullable 표현 세 가지뿐이다.
"""
import json
import sys


def convert(node):
    if isinstance(node, list):
        return [convert(x) for x in node]
    if not isinstance(node, dict):
        return node

    out = {}
    for key, value in node.items():
        out[key] = convert(value)

    t = out.get("type")
    if isinstance(t, list):
        non_null = [x for x in t if x != "null"]
        had_null = len(non_null) != len(t)
        if len(non_null) == 1:
            out["type"] = non_null[0]
        else:
            # 다중 타입 유니온(임의 JSON 값)은 3.0 으로 표현할 수 없다 — 타입 무제약으로 낮춘다
            del out["type"]
        if had_null:
            out["nullable"] = True
    elif t == "null":
        # 항상 null 인 필드(빈 data). 3.0 에는 null 타입이 없다
        del out["type"]
        out["nullable"] = True

    one_of = out.get("oneOf")
    if isinstance(one_of, list) and len(one_of) == 2:
        refs = [x for x in one_of if "$ref" in x]
        nulls = [x for x in one_of if x.get("type") == "null"]
        if len(refs) == 1 and len(nulls) == 1:
            del out["oneOf"]
            out["allOf"] = [refs[0]]
            out["nullable"] = True

    excl = out.get("exclusiveMinimum")
    if isinstance(excl, (int, float)) and not isinstance(excl, bool):
        out["minimum"] = excl
        out["exclusiveMinimum"] = True

    return out


def main():
    src, dst = sys.argv[1], sys.argv[2]
    with open(src) as f:
        spec = json.load(f)
    spec = convert(spec)
    spec["openapi"] = "3.0.3"
    with open(dst, "w") as f:
        json.dump(spec, f, ensure_ascii=False, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
