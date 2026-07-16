# -*- coding: utf-8 -*-
"""把 GROK CPA 导出的 xai-*.json 账号转成 sub2api 导入用的 accounts.json。

用法:
    python cap_to_sub2api_accounts.py <源目录> [输出文件] [--base-url URL]

源目录里每个 xai-*.json 是一个 xAI OAuth 账号(access_token/refresh_token/id_token/
email/sub/expired 等)。client_id/team_id/scope 从 access_token 的 JWT 载荷解出。
输出是 sub2api 面板「导入」用的 DataPayload 格式;proxies 为空,accounts 为转换结果。

默认 base_url 用 CLI 代理端点(与 sub2api BuildAccountCredentials 一致),而非源文件的
api.x.ai/v1 —— sub2api 的 grok OAuth 转发与 CLI header 逻辑按此端点设计。
"""
import json, glob, os, base64, sys, argparse, datetime

DEFAULT_BASE_URL = "https://cli-chat-proxy.grok.com/v1"


def jwt_payload(tok):
    try:
        p = tok.split(".")[1]
        p += "=" * (-len(p) % 4)
        return json.loads(base64.urlsafe_b64decode(p))
    except Exception:
        return {}


def convert(src_dir, base_url):
    accounts, skipped = [], []
    for f in sorted(glob.glob(os.path.join(src_dir, "xai-*.json"))):
        try:
            d = json.load(open(f, encoding="utf-8"))
        except Exception as e:
            skipped.append((os.path.basename(f), f"读取失败:{e}")); continue
        at = d.get("access_token", "")
        if not at:
            skipped.append((os.path.basename(f), "无 access_token")); continue
        claims = jwt_payload(at)
        email = d.get("email") or claims.get("email") or os.path.basename(f)
        accounts.append({
            "name": email,
            "platform": "grok",
            "type": "oauth",
            "credentials": {
                "access_token":  at,
                "refresh_token": d.get("refresh_token", ""),
                "id_token":      d.get("id_token", ""),
                "token_type":    d.get("token_type", "Bearer"),
                "client_id":     claims.get("client_id", ""),
                "team_id":       claims.get("team_id", ""),
                "scope":         claims.get("scope", ""),
                "email":         email,
                "sub":           d.get("sub") or claims.get("sub", ""),
                "expires_at":    d.get("expired", ""),
                "base_url":      base_url,
            },
            "concurrency": 1,
            "priority": 0,
        })
    return accounts, skipped


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("src_dir", help="含 xai-*.json 的源目录")
    ap.add_argument("out", nargs="?", default="sub2api-grok-accounts.json", help="输出文件")
    ap.add_argument("--base-url", default=DEFAULT_BASE_URL)
    args = ap.parse_args()

    if not os.path.isdir(args.src_dir):
        print("源目录不存在:", args.src_dir); sys.exit(1)

    accounts, skipped = convert(args.src_dir, args.base_url)
    payload = {
        "type": "sub2api-data",
        "version": 1,
        "exported_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "proxies": [],
        "accounts": accounts,
    }
    json.dump(payload, open(args.out, "w", encoding="utf-8"), ensure_ascii=False, indent=2)

    names = [a["name"] for a in accounts]
    print(f"转换 {len(accounts)} 个账号(唯一名 {len(set(names))})-> {args.out}")
    if skipped:
        print(f"跳过 {len(skipped)} 个:")
        for n, r in skipped:
            print(f"  {n}: {r}")


if __name__ == "__main__":
    main()
