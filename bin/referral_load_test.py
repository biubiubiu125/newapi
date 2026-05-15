#!/usr/bin/env python3
import concurrent.futures
import http.cookiejar
import json
import os
import time
import urllib.error
import urllib.request


BASE_URL = os.environ.get("REFERRAL_BASE_URL", "").rstrip("/")
ADMIN_USERNAME = os.environ.get("REFERRAL_ADMIN_USERNAME", "")
ADMIN_PASSWORD = os.environ.get("REFERRAL_ADMIN_PASSWORD", "")
ADMIN_USER_ID = os.environ.get("REFERRAL_ADMIN_USER_ID", "1")
ADMIN_TOKEN = os.environ.get("REFERRAL_ADMIN_TOKEN", "")
USER_PASSWORD = os.environ.get("REFERRAL_USER_PASSWORD", "")
TOPUP_PRODUCT_ID = os.environ.get("REFERRAL_TOPUP_PRODUCT_ID", "creem_topup_100")
TOPUP_EVENT_SIGNATURE = os.environ.get("REFERRAL_EVENT_SIGNATURE", "test")
MODE = os.environ.get("REFERRAL_LOAD_MODE", "topup-webhook-idempotency").strip()
RUN_ID = str(int(time.time()))


def require_env(name: str, value: str) -> None:
    if value:
        return
    raise SystemExit(f"missing required env: {name}")


class Client:
    def __init__(self) -> None:
        self.user_id = None
        self.auth_token = ""
        self.cookie_jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.cookie_jar)
        )

    def request(self, method: str, path: str, payload=None, headers=None, expect_json=True):
        data = None if payload is None else json.dumps(payload).encode()
        req = urllib.request.Request(
            BASE_URL + path,
            data=data,
            headers=headers or {},
            method=method,
        )
        try:
            with self.opener.open(req, timeout=30) as resp:
                body = resp.read().decode("utf-8", "ignore")
                if not expect_json:
                    return resp.getcode(), body
                if not body.strip():
                    parsed = {"success": True, "message": "", "data": None}
                else:
                    parsed = json.loads(body)
                return resp.getcode(), parsed
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", "ignore")
            if not expect_json:
                return exc.code, body
            try:
                parsed = json.loads(body)
            except json.JSONDecodeError:
                parsed = {"success": False, "message": body}
            return exc.code, parsed

    def login(self, username: str, password: str) -> None:
        status, resp = self.request(
            "POST",
            "/api/user/login",
            {"username": username, "password": password},
            {"Content-Type": "application/json"},
        )
        if status != 200 or not resp.get("success"):
            raise RuntimeError(f"login failed for {username}: {status} {resp}")
        self.user_id = resp["data"]["id"]

    def authed(self, method: str, path: str, payload=None):
        if self.user_id is None:
            raise RuntimeError("user not logged in")
        headers = {
            "Content-Type": "application/json",
            "New-Api-User": str(self.user_id),
        }
        if self.auth_token:
            headers["Authorization"] = self.auth_token
        return self.request(method, path, payload, headers)


def admin_client() -> Client:
    client = Client()
    if ADMIN_TOKEN:
        client.user_id = int(ADMIN_USER_ID)
        client.auth_token = ADMIN_TOKEN
        return client
    client.login(ADMIN_USERNAME, ADMIN_PASSWORD)
    return client


def short_name(prefix: str, suffix: str) -> str:
    raw = f"{prefix}_{suffix}"
    return raw[:20]


def create_affiliate(admin: Client, username: str) -> tuple[Client, str]:
    affiliate = Client()
    status, resp = affiliate.request(
        "POST",
        "/api/user/register",
        {"username": username, "password": USER_PASSWORD},
        {"Content-Type": "application/json"},
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"register affiliate failed: {status} {resp}")
    affiliate.login(username, USER_PASSWORD)
    status, resp = affiliate.authed(
        "POST", "/api/user/referral/apply", {"applicant_note": f"load {username}"}
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"apply affiliate failed: {status} {resp}")
    status, resp = admin.authed(
        "POST",
        f"/api/user/admin/referral/affiliates/{affiliate.user_id}/approve",
        {"rate_override": 15},
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"approve affiliate failed: {status} {resp}")
    status, resp = affiliate.authed("GET", "/api/user/referral/summary")
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"get summary failed: {status} {resp}")
    return affiliate, resp["data"]["invite_code"]


def create_invitee(invite_code: str, username: str) -> Client:
    invitee = Client()
    invitee.request("GET", f"/api/r/{invite_code}?redirect=/register", expect_json=False)
    status, resp = invitee.request(
        "POST",
        "/api/user/register",
        {"username": username, "password": USER_PASSWORD},
        {"Content-Type": "application/json"},
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"register invitee failed: {status} {resp}")
    invitee.login(username, USER_PASSWORD)
    return invitee


def create_topup_order(invitee: Client) -> str:
    status, resp = invitee.authed(
        "POST",
        "/api/user/creem/pay",
        {"product_id": TOPUP_PRODUCT_ID, "payment_method": "creem"},
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"create topup order failed: {status} {resp}")
    return resp["data"]["order_id"]


def build_topup_event(order_id: str, user_suffix: str) -> dict:
    return {
        "id": f"evt_{order_id}",
        "eventType": "checkout.completed",
        "created_at": int(time.time()),
        "object": {
            "id": f"obj_{order_id}",
            "request_id": order_id,
            "order": {
                "object": "order",
                "id": f"creem_order_{order_id}",
                "customer": f"cust_{user_suffix}",
                "product": TOPUP_PRODUCT_ID,
                "amount": 10000,
                "currency": "USD",
                "sub_total": 10000,
                "tax_amount": 0,
                "amount_due": 10000,
                "amount_paid": 10000,
                "status": "paid",
                "type": "onetime",
                "transaction": f"txn_{order_id}",
                "created_at": "",
                "updated_at": "",
                "mode": "test",
            },
            "product": {
                "id": TOPUP_PRODUCT_ID,
                "object": "product",
                "name": "Topup 100",
                "description": "",
                "price": 10000,
                "currency": "USD",
                "billing_type": "one_time",
                "billing_period": "",
                "status": "active",
                "tax_mode": "",
                "tax_category": "",
                "default_success_url": None,
                "created_at": "",
                "updated_at": "",
                "mode": "test",
            },
            "units": 1,
            "customer": {
                "id": f"cust_{user_suffix}",
                "object": "customer",
                "email": f"{user_suffix}@example.test",
                "name": user_suffix,
                "country": "US",
                "created_at": "",
                "updated_at": "",
                "mode": "test",
            },
            "status": "paid",
            "metadata": {},
            "mode": "test",
        },
    }


def post_creem_webhook(event: dict) -> tuple[int, str]:
    body = json.dumps(event).encode()
    req = urllib.request.Request(
        BASE_URL + "/api/creem/webhook",
        data=body,
        headers={
            "Content-Type": "application/json",
            "creem-signature": TOPUP_EVENT_SIGNATURE,
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.getcode(), resp.read().decode("utf-8", "ignore")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", "ignore")


def list_admin_commissions(admin: Client):
    status, resp = admin.authed(
        "GET", "/api/user/admin/referral/commissions?p=1&page_size=500"
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"list commissions failed: {status} {resp}")
    return resp["data"]["items"]


def list_admin_ledgers(admin: Client):
    status, resp = admin.authed(
        "GET", "/api/user/admin/referral/ledgers?p=1&page_size=500"
    )
    if status != 200 or not resp.get("success"):
        raise RuntimeError(f"list ledgers failed: {status} {resp}")
    return resp["data"]["items"]


def run_topup_webhook_idempotency() -> None:
    admin = admin_client()
    affiliate, invite_code = create_affiliate(admin, short_name("aload", RUN_ID))
    invitee = create_invitee(invite_code, short_name("iload", RUN_ID))
    order_id = create_topup_order(invitee)
    event = build_topup_event(order_id, short_name("eload", RUN_ID))

    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
        results = list(executor.map(lambda _: post_creem_webhook(event), range(10)))
    elapsed = time.perf_counter() - started

    commissions = [
        item for item in list_admin_commissions(admin) if item["source_trade_no"] == order_id
    ]
    ledgers = [
        item
        for item in list_admin_ledgers(admin)
        if item["external_ref_id"] == f"accrue:topup:{order_id}"
    ]
    print(
        json.dumps(
            {
                "mode": MODE,
                "order_id": order_id,
                "webhook_results": results,
                "commission_count": len(commissions),
                "ledger_count": len(ledgers),
                "elapsed_seconds": round(elapsed, 3),
            },
            ensure_ascii=False,
        )
    )


def run_settlement_concurrency() -> None:
    admin = admin_client()
    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
        results = list(
            executor.map(
                lambda _: admin.authed("POST", "/api/user/admin/referral/settlements/run"),
                range(10),
            )
        )
    elapsed = time.perf_counter() - started
    print(
        json.dumps(
            {
                "mode": MODE,
                "results": results,
                "elapsed_seconds": round(elapsed, 3),
            },
            ensure_ascii=False,
        )
    )


def run_withdrawal_idempotency() -> None:
    admin = admin_client()
    affiliate, invite_code = create_affiliate(admin, short_name("awd", RUN_ID))
    invitee = create_invitee(invite_code, short_name("iwd", RUN_ID))
    trade_no = create_topup_order(invitee)
    event = build_topup_event(trade_no, short_name("ewd", RUN_ID))
    post_creem_webhook(event)
    admin.authed("POST", "/api/user/admin/referral/settlements/run")

    payload = {
        "amount": 5,
        "account_type": "alipay",
        "account_name": "Load Tester",
        "account_no": "load-account",
        "account_network": "",
        "qr_image_url": "",
        "applicant_note": "load test withdraw",
        "idempotency_key": f"wd-{RUN_ID}",
    }
    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
        results = list(
            executor.map(
                lambda _: affiliate.authed("POST", "/api/user/referral/withdrawals", payload),
                range(5),
            )
        )
    elapsed = time.perf_counter() - started
    withdrawal_ids = []
    for _, resp in results:
        if isinstance(resp, dict) and resp.get("success") and resp.get("data"):
            withdrawal_ids.append(resp["data"]["id"])
    print(
        json.dumps(
            {
                "mode": MODE,
                "results": results,
                "withdrawal_ids": sorted(set(withdrawal_ids)),
                "elapsed_seconds": round(elapsed, 3),
            },
            ensure_ascii=False,
        )
    )


def run_binding_registration_concurrency() -> None:
    admin = admin_client()
    _, invite_code = create_affiliate(admin, short_name("abind", RUN_ID))

    def worker(index: int):
        username = short_name(f"ib{index}", RUN_ID)
        client = Client()
        client.request("GET", f"/api/r/{invite_code}?redirect=/register", expect_json=False)
        status, resp = client.request(
            "POST",
            "/api/user/register",
            {"username": username, "password": USER_PASSWORD},
            {"Content-Type": "application/json"},
        )
        return {
            "index": index,
            "status": status,
            "success": isinstance(resp, dict) and resp.get("success", False),
            "message": resp.get("message", "") if isinstance(resp, dict) else "",
        }

    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=20) as executor:
        results = list(executor.map(worker, range(20)))
    elapsed = time.perf_counter() - started
    success_count = sum(1 for item in results if item["success"])
    print(
        json.dumps(
            {
                "mode": MODE,
                "requested": len(results),
                "success_count": success_count,
                "failure_count": len(results) - success_count,
                "results": results,
                "elapsed_seconds": round(elapsed, 3),
            },
            ensure_ascii=False,
        )
    )


def main() -> None:
    require_env("REFERRAL_BASE_URL", BASE_URL)
    if not ADMIN_TOKEN:
        require_env("REFERRAL_ADMIN_USERNAME", ADMIN_USERNAME)
        require_env("REFERRAL_ADMIN_PASSWORD", ADMIN_PASSWORD)
    require_env("REFERRAL_USER_PASSWORD", USER_PASSWORD)
    if MODE == "topup-webhook-idempotency":
        run_topup_webhook_idempotency()
        return
    if MODE == "settlement-concurrency":
        run_settlement_concurrency()
        return
    if MODE == "withdrawal-idempotency":
        run_withdrawal_idempotency()
        return
    if MODE == "binding-registration-concurrency":
        run_binding_registration_concurrency()
        return
    raise SystemExit(f"unsupported REFERRAL_LOAD_MODE: {MODE}")


if __name__ == "__main__":
    main()
