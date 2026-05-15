import concurrent.futures
import json
import os
import sys
import time
import urllib.error
import urllib.request


BASE_URL = os.environ.get("REFERRAL_BASE_URL", "").rstrip("/")
ADMIN_TOKEN = os.environ.get("REFERRAL_ADMIN_TOKEN", "")
ADMIN_USER_ID = os.environ.get("REFERRAL_ADMIN_USER_ID", "")
AFFILIATE_TOKEN = os.environ.get("REFERRAL_AFFILIATE_TOKEN", "")
AFFILIATE_USER_ID = os.environ.get("REFERRAL_AFFILIATE_USER_ID", "")


def require_env(name: str, value: str) -> None:
    if value:
        return
    raise SystemExit(f"missing required env: {name}")


def request(method: str, path: str, payload=None, headers=None):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(
        BASE_URL + path,
        data=data,
        headers=headers or {},
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = resp.read().decode("utf-8", "ignore")
            return resp.getcode(), json.loads(body)
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", "ignore")
        try:
            parsed = json.loads(body)
        except json.JSONDecodeError:
            parsed = {"success": False, "message": body}
        return exc.code, parsed


def admin_headers():
    return {
        "Content-Type": "application/json",
        "Authorization": ADMIN_TOKEN,
        "New-Api-User": str(ADMIN_USER_ID),
    }


def affiliate_headers():
    return {
        "Content-Type": "application/json",
        "Authorization": AFFILIATE_TOKEN,
        "New-Api-User": str(AFFILIATE_USER_ID),
    }


def main():
    require_env("REFERRAL_BASE_URL", BASE_URL)
    require_env("REFERRAL_ADMIN_TOKEN", ADMIN_TOKEN)
    require_env("REFERRAL_ADMIN_USER_ID", ADMIN_USER_ID)
    require_env("REFERRAL_AFFILIATE_TOKEN", AFFILIATE_TOKEN)
    require_env("REFERRAL_AFFILIATE_USER_ID", AFFILIATE_USER_ID)

    status, summary = request("GET", "/api/user/referral/summary", headers=affiliate_headers())
    print("affiliate_summary", status, json.dumps(summary, ensure_ascii=False))

    status, ledgers = request("GET", "/api/user/admin/referral/ledgers?p=1&page_size=50", headers=admin_headers())
    print("ledger_page", status, json.dumps(ledgers, ensure_ascii=False))


if __name__ == "__main__":
    main()
