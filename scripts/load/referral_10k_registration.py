#!/usr/bin/env python3
import concurrent.futures
import collections
import json
import os
import statistics
import time
import urllib.error
import urllib.request


BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:3000").rstrip("/")
PROMOTERS = int(os.environ.get("PROMOTERS", "100"))
INVITEES_PER_PROMOTER = int(os.environ.get("INVITEES_PER_PROMOTER", "100"))
WORKERS = int(os.environ.get("WORKERS", "1000"))
RUN_ID = os.environ.get("RUN_ID", str(int(time.time())))
PASSWORD = os.environ.get("TEST_PASSWORD", "AuditLoad123!")
INVITE_CODES = [code.strip() for code in os.environ.get("INVITE_CODES", "").split(",") if code.strip()]


def register_one(index):
    promoter_index = index // INVITEES_PER_PROMOTER
    invitee_index = index % INVITEES_PER_PROMOTER
    username = f"u{RUN_ID[-4:]}{promoter_index:03d}{invitee_index:03d}"
    payload = {
        "username": username,
        "password": PASSWORD,
        "email": f"load_invitee_{promoter_index}_{invitee_index}_{RUN_ID}@example.test",
        "aff": INVITE_CODES[promoter_index],
    }
    body = json.dumps(payload).encode()
    started = time.time()
    status = 0
    success = False
    error = ""
    try:
        request = urllib.request.Request(
            f"{BASE_URL}/api/user/register",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        response = urllib.request.urlopen(request, timeout=float(os.environ.get("REQUEST_TIMEOUT", "30")))
        text = response.read().decode("utf-8", "replace")
        status = response.status
        data = json.loads(text)
        success = bool(data.get("success"))
        if not success:
            error = str(data.get("message") or text[:120])
    except urllib.error.HTTPError as exc:
        status = exc.code
        error = exc.read().decode("utf-8", "replace")[:120]
    except Exception as exc:
        error = f"{type(exc).__name__}: {str(exc)[:100]}"
    return {
        "status": status,
        "success": success,
        "ms": (time.time() - started) * 1000,
        "error": error,
    }


def percentile(values, pct):
    if not values:
        return 0
    index = min(len(values) - 1, int(len(values) * pct / 100))
    return values[index]


def main():
    if len(INVITE_CODES) < PROMOTERS:
        raise SystemExit(f"INVITE_CODES has {len(INVITE_CODES)} codes, need {PROMOTERS}")
    total = PROMOTERS * INVITEES_PER_PROMOTER
    started = time.time()
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=WORKERS) as executor:
        for result in executor.map(register_one, range(total)):
            results.append(result)
    latencies = sorted(item["ms"] for item in results)
    summary = {
        "run_id": RUN_ID,
        "promoters": PROMOTERS,
        "invitees_per_promoter": INVITEES_PER_PROMOTER,
        "total": total,
        "workers": WORKERS,
        "duration_sec": round(time.time() - started, 3),
        "success": sum(1 for item in results if item["success"]),
        "failed": sum(1 for item in results if not item["success"]),
        "status_codes": dict(collections.Counter(str(item["status"]) for item in results)),
        "avg_ms": round(statistics.mean(latencies), 2) if latencies else 0,
        "p95_ms": round(percentile(latencies, 95), 2),
        "p99_ms": round(percentile(latencies, 99), 2),
        "max_ms": round(max(latencies), 2) if latencies else 0,
        "errors": dict(collections.Counter(item["error"] for item in results if not item["success"]).most_common(10)),
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
