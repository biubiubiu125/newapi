#!/usr/bin/env python3
import concurrent.futures
import collections
import hashlib
import http.cookiejar
import json
import os
import statistics
import time
import urllib.error
import urllib.parse
import urllib.request


BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:3000").rstrip("/")
TEST_PASSWORD = os.environ.get("TEST_PASSWORD", "AuditPay123!")
EPAY_PID = os.environ.get("EPAY_PID", "audit_epay_pid")
EPAY_KEY = os.environ.get("EPAY_KEY", "audit_epay_key_for_signed_callback_only")
EPUSDT_PID = os.environ.get("EPUSDT_PID", "audit_epusdt_pid")
EPUSDT_KEY = os.environ.get("EPUSDT_KEY", "audit_epusdt_key_for_signed_callback_only")
RUN_ID = os.environ.get("RUN_ID", str(int(time.time())))
PROMOTERS = int(os.environ.get("PROMOTERS", "10"))
INVITEES_PER_PROMOTER = int(os.environ.get("INVITEES_PER_PROMOTER", "100"))
WORKERS = int(os.environ.get("WORKERS", "200"))


class Session:
    def __init__(self):
        jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def request(self, method, path, data=None, headers=None, form=False, timeout=30):
        headers = dict(headers or {})
        body = None
        if data is not None:
            if form:
                body = urllib.parse.urlencode(data).encode()
                headers.setdefault("Content-Type", "application/x-www-form-urlencoded")
            else:
                body = json.dumps(data).encode()
                headers.setdefault("Content-Type", "application/json")
        request = urllib.request.Request(f"{BASE_URL}{path}", data=body, headers=headers, method=method)
        try:
            response = self.opener.open(request, timeout=timeout)
            text = response.read().decode("utf-8", "replace")
            status = response.status
        except urllib.error.HTTPError as exc:
            text = exc.read().decode("utf-8", "replace")
            status = exc.code
        try:
            parsed = json.loads(text)
        except Exception:
            parsed = {"raw": text}
        return status, parsed, text


def epay_signature(params):
    filtered = {key: value for key, value in params.items() if key not in ("sign", "sign_type") and str(value) != ""}
    payload = "&".join(f"{key}={filtered[key]}" for key in sorted(filtered)) + EPAY_KEY
    signed = dict(params)
    signed["sign"] = hashlib.md5(payload.encode()).hexdigest()
    signed["sign_type"] = "MD5"
    return signed


def epusdt_signature(params):
    filtered = {key: value for key, value in params.items() if key not in ("signature", "sign") and str(value) != ""}
    payload = "&".join(f"{key}={filtered[key]}" for key in sorted(filtered)) + EPUSDT_KEY
    signed = dict(params)
    signed["signature"] = hashlib.md5(payload.encode()).hexdigest()
    return signed


def create_promoters():
    codes = []
    for index in range(PROMOTERS):
        username = f"q{index:02d}{RUN_ID[-4:]}"
        session = Session()
        session.request("POST", "/api/user/register", {"username": username, "password": TEST_PASSWORD})
        _, login, _ = session.request("POST", "/api/user/login", {"username": username, "password": TEST_PASSWORD})
        user_id = login["data"]["id"]
        _, profile, _ = session.request("POST", "/api/user/referral/apply", {}, {"New-Api-User": str(user_id)})
        codes.append(profile["data"]["invite_code"])
    return codes


def create_invitees(codes):
    users = []
    for promoter_index, code in enumerate(codes):
        for user_index in range(INVITEES_PER_PROMOTER):
            username = f"v{RUN_ID[-4:]}{promoter_index:02d}{user_index:03d}"
            session = Session()
            session.request("POST", "/api/user/register", {"username": username, "password": TEST_PASSWORD, "aff": code})
            _, login, _ = session.request("POST", "/api/user/login", {"username": username, "password": TEST_PASSWORD})
            users.append((login["data"]["id"], session))
    return users


def pay_one(index_and_user):
    index, (user_id, session) = index_and_user
    provider = "epay" if index % 2 == 0 else "epusdt"
    started = time.time()
    try:
        if provider == "epay":
            _, order, _ = session.request("POST", "/api/user/pay", {"amount": 10, "payment_method": "alipay"}, {"New-Api-User": str(user_id)})
            trade_no = order["data"]["out_trade_no"]
            callback = epay_signature({
                "pid": EPAY_PID,
                "trade_no": f"mix_{trade_no}",
                "out_trade_no": trade_no,
                "type": "alipay",
                "name": "audit",
                "money": "73.00",
                "trade_status": "TRADE_SUCCESS",
            })
            _, _, text = Session().request("POST", "/api/user/epay/notify", callback, form=True)
            ok = text == "success"
        else:
            _, order, _ = session.request("POST", "/api/user/epusdt/pay", {"amount": 10, "payment_method": "epusdt:usdt:tron"}, {"New-Api-User": str(user_id)})
            trade_no = order["data"]["order_id"]
            callback = epusdt_signature({
                "pid": EPUSDT_PID,
                "order_id": trade_no,
                "status": "paid",
                "amount": "73.00",
                "settlement_currency": "CNY",
                "token": "usdt",
                "network": "tron",
                "transaction_id": f"mix_{trade_no[-8:]}",
            })
            _, _, text = Session().request("POST", "/api/user/epusdt/notify", callback)
            ok = text == "ok"
        return {"ok": ok, "provider": provider, "trade_no": trade_no, "ms": (time.time() - started) * 1000, "error": ""}
    except Exception as exc:
        return {"ok": False, "provider": provider, "trade_no": "", "ms": (time.time() - started) * 1000, "error": f"{type(exc).__name__}: {str(exc)[:100]}"}


def percentile(values, pct):
    if not values:
        return 0
    return values[min(len(values) - 1, int(len(values) * pct / 100))]


def main():
    codes = create_promoters()
    users = create_invitees(codes)
    started = time.time()
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=WORKERS) as executor:
        for result in executor.map(pay_one, enumerate(users)):
            results.append(result)
    latencies = sorted(item["ms"] for item in results)
    summary = {
        "run_id": RUN_ID,
        "total": len(results),
        "success": sum(1 for item in results if item["ok"]),
        "failed": sum(1 for item in results if not item["ok"]),
        "duration_sec": round(time.time() - started, 3),
        "providers": dict(collections.Counter(f"{item['provider']}:{item['ok']}" for item in results)),
        "avg_ms": round(statistics.mean(latencies), 2) if latencies else 0,
        "p95_ms": round(percentile(latencies, 95), 2),
        "p99_ms": round(percentile(latencies, 99), 2),
        "max_ms": round(max(latencies), 2) if latencies else 0,
        "errors": dict(collections.Counter(item["error"] for item in results if not item["ok"]).most_common(10)),
        "trade_numbers": [item["trade_no"] for item in results if item["trade_no"]],
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
