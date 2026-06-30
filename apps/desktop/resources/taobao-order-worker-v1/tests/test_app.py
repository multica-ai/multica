import os
import sys
from pathlib import Path

import pytest
from fastapi import HTTPException
from fastapi.testclient import TestClient


os.environ.setdefault("ORDER_BRIDGE_API_TOKEN", "test-token")
os.environ.setdefault("REQUIRE_ORDER_BRIDGE_API_AUTH", "true")
os.environ.setdefault("TAOBAO_EVENT_SECRET", "test-secret")
os.environ.setdefault("DEDUPE_DB_PATH", "/tmp/taobao-order-worker-test.sqlite3")

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import app as app_module  # noqa: E402


BRIDGE_HEADERS = {"X-Order-Bridge-Token": "test-token"}
EVENT_HEADERS = {"X-Order-Event-Secret": "test-secret"}


@pytest.fixture
def client(tmp_path, monkeypatch):
    monkeypatch.setattr(app_module, "store", app_module.Store(str(tmp_path / "order_worker.sqlite3")))
    monkeypatch.setattr(app_module, "TAOBAO_EVENT_SECRET", "test-secret")
    monkeypatch.setattr(app_module, "ORDER_BRIDGE_API_TOKEN", "test-token")
    monkeypatch.setattr(app_module, "REQUIRE_ORDER_BRIDGE_API_AUTH", True)
    monkeypatch.setattr(app_module, "ENV", "dev")
    monkeypatch.setattr(app_module, "MULTICA_AUTOPILOT_WEBHOOK_URL", "")
    monkeypatch.setattr(app_module, "ORDER_API_BASE_URL", "")
    monkeypatch.setattr(app_module, "ORDER_API_WRITE_THROUGH", False)
    monkeypatch.setattr(app_module, "STORE_PLAIN_RECEIVER_IN_ACTION_LOG", False)
    return TestClient(app_module.app)


def taobao_event(event_id="evt-1", tid="1234567890"):
    return {
        "event": "taobao.trade.modified",
        "eventId": event_id,
        "eventPayload": {
            "platform": "taobao",
            "shop_id": "shop_001",
            "tid": tid,
            "status": "WAIT_SELLER_SEND_GOODS",
            "modified": "2026-06-30T10:12:00+08:00",
            "use_plain_receiver_info": True,
            "idempotency_key": f"taobao:shop_001:{tid}:modified:v1",
        },
    }


def shipping_payload(key="ship-key"):
    return {
        "idempotency_key": key,
        "warehouse_id": "WH_HZ_001",
        "logistics_company": "YTO",
        "package_note": "AI检查通过，创建发货草稿",
        "actor": "taobao-order-ops",
    }


def check(client, tid):
    resp = client.post(f"/api/orders/{tid}/check-fulfillment", json={"plain": True}, headers=BRIDGE_HEADERS)
    assert resp.status_code == 200
    return resp.json()


def test_health_and_event_secret_dedupe(client):
    assert client.get("/health").status_code == 200

    bad = client.post("/taobao/order-event", json=taobao_event(), headers={"X-Order-Event-Secret": "bad"})
    assert bad.status_code == 401

    ok = client.post("/taobao/order-event", json=taobao_event(), headers=EVENT_HEADERS)
    assert ok.status_code == 200
    assert ok.json()["deduped"] is False

    deduped = client.post("/taobao/order-event", json=taobao_event(), headers=EVENT_HEADERS)
    assert deduped.status_code == 200
    assert deduped.json()["deduped"] is True


def test_multica_webhook_failure_allows_event_retry(client, monkeypatch):
    seen_payloads = []

    async def failing_webhook(payload):
        seen_payloads.append(payload)
        raise HTTPException(status_code=502, detail={"message": "Multica webhook returned non-2xx", "status_code": 500})

    monkeypatch.setattr(app_module, "post_multica_autopilot", failing_webhook)
    payload = taobao_event(event_id="evt-retry", tid="retry-tid")
    failed = client.post("/taobao/order-event", json=payload, headers=EVENT_HEADERS)
    assert failed.status_code == 502
    assert app_module.store.get_event("evt-retry")["status"] == "failed"

    async def successful_webhook(payload):
        seen_payloads.append(payload)
        return {"status_code": 200, "body": "ok"}

    monkeypatch.setattr(app_module, "post_multica_autopilot", successful_webhook)
    retried = client.post("/taobao/order-event", json=payload, headers=EVENT_HEADERS)
    assert retried.status_code == 200
    assert retried.json()["deduped"] is False
    assert app_module.store.get_event("evt-retry")["status"] == "sent"
    assert "eventPayload" in seen_payloads[-1]
    assert "trigger_payload" not in seen_payloads[-1]


def test_prod_requires_multica_webhook_and_event_is_failed(client, monkeypatch):
    monkeypatch.setattr(app_module, "ENV", "prod")
    monkeypatch.setattr(app_module, "MULTICA_AUTOPILOT_WEBHOOK_URL", "")

    with pytest.raises(RuntimeError, match="MULTICA_AUTOPILOT_WEBHOOK_URL"):
        app_module.validate_runtime_config()

    payload = taobao_event(event_id="evt-prod-empty-webhook", tid="prod-tid")
    failed = client.post("/taobao/order-event", json=payload, headers=EVENT_HEADERS)
    assert failed.status_code == 500
    assert app_module.store.get_event("evt-prod-empty-webhook")["status"] == "failed"


def test_event_reservation_blocks_pending_duplicate(client):
    payload = taobao_event(event_id="evt-pending", tid="pending-tid")
    first_status, first_result = app_module.store.reserve_event(
        "evt-pending",
        "pending-tid",
        "idem-pending",
        payload,
    )
    assert first_status == "reserved"
    assert first_result is None

    second_status, second_result = app_module.store.reserve_event(
        "evt-pending",
        "pending-tid",
        "idem-pending",
        payload,
    )
    assert second_status == "pending"
    assert second_result == {}

    duplicate = client.post("/taobao/order-event", json=payload, headers=EVENT_HEADERS)
    assert duplicate.status_code == 409


def test_order_plain_requires_bridge_token(client):
    no_token = client.get("/api/orders/1234567890?plain=true")
    assert no_token.status_code == 401

    ok = client.get("/api/orders/1234567890?plain=true", headers=BRIDGE_HEADERS)
    assert ok.status_code == 200
    assert ok.json()["buyer"]["receiver_mobile"] == "13888888888"


def test_fulfillment_status_decisions(client):
    waitpay = check(client, "123waitpay")
    assert waitpay["required_action"] == "ignore"
    assert waitpay["risk_level"] == "low"

    normal = check(client, "1234567890")
    assert normal["can_ship"] is True
    assert normal["required_action"] == "create_shipping_draft"
    assert "safe_summary" in normal
    assert normal["metadata"]["tid"] == "1234567890"
    assert normal["metadata"]["fulfillment_status"] == "shipping_draft_ready"

    refund = check(client, "123refund")
    assert refund["required_action"] == "hold"
    assert refund["risk_level"] == "high"
    assert "REFUND_IN_PROGRESS" in refund["reason_codes"]

    forbid = check(client, "123forbid")
    assert forbid["required_action"] == "hold"
    assert "PAID_FORBID_CONSIGN" in forbid["reason_codes"]

    stockout = check(client, "123stockout")
    assert stockout["required_action"] == "hold"
    assert "INVENTORY_NOT_RESERVED" in stockout["reason_codes"]


def test_manual_review_and_area_decisions(client):
    address = check(client, "123address")
    assert address["required_action"] == "manual_review"
    assert "ADDRESS_INCOMPLETE" in address["reason_codes"]

    message = check(client, "123message")
    assert message["required_action"] == "manual_review"
    assert "MESSAGE_REQUIRES_MANUAL_REVIEW" in message["reason_codes"]

    sku = check(client, "123sku")
    assert sku["required_action"] == "manual_review"
    assert "SKU_MAPPING_MISSING" in sku["reason_codes"]

    mobile = check(client, "123mobile")
    assert mobile["required_action"] == "manual_review"
    assert "RECEIVER_MOBILE_INVALID" in mobile["reason_codes"]

    remote = check(client, "123remote")
    assert remote["required_action"] == "manual_review"
    assert remote["receiver_check"]["remote_area"] is True

    unsupported = check(client, "123unsupported")
    assert unsupported["required_action"] == "hold"
    assert unsupported["risk_level"] == "high"
    assert "LOGISTICS_UNSUPPORTED_AREA" in unsupported["reason_codes"]


def test_create_shipping_draft_success_dedupe_and_failed_check(client):
    created = client.post(
        "/api/orders/1234567890/create-shipping-draft",
        json=shipping_payload("ship-ok"),
        headers=BRIDGE_HEADERS,
    )
    assert created.status_code == 200
    result = created.json()
    assert result["deduped"] is False
    assert "receiver_summary" in result["result"]["local"]
    assert "receiver" not in result["result"]["local"]

    deduped = client.post(
        "/api/orders/1234567890/create-shipping-draft",
        json=shipping_payload("ship-ok"),
        headers=BRIDGE_HEADERS,
    )
    assert deduped.status_code == 200
    assert deduped.json()["deduped"] is True

    blocked = client.post(
        "/api/orders/123refund/create-shipping-draft",
        json=shipping_payload("ship-refund"),
        headers=BRIDGE_HEADERS,
    )
    assert blocked.status_code == 409
    assert app_module.store.get_action_record("ship-refund")["status"] == "failed"


def test_upstream_write_failure_is_not_recorded_as_success(client, monkeypatch):
    monkeypatch.setattr(app_module, "ORDER_API_WRITE_THROUGH", True)
    monkeypatch.setattr(app_module, "ORDER_API_BASE_URL", "")
    missing_base_url = client.post(
        "/api/orders/1234567890/create-shipping-draft",
        json=shipping_payload("ship-missing-upstream-base"),
        headers=BRIDGE_HEADERS,
    )
    assert missing_base_url.status_code == 500
    assert app_module.store.get_action_record("ship-missing-upstream-base")["status"] == "failed"

    monkeypatch.setattr(app_module, "ORDER_API_WRITE_THROUGH", False)

    async def failing_write(method, path, payload):
        raise HTTPException(status_code=502, detail={"message": "order API write returned non-2xx", "status_code": 500})

    monkeypatch.setattr(app_module, "upstream_write", failing_write)
    failed = client.post(
        "/api/orders/1234567890/create-shipping-draft",
        json=shipping_payload("ship-upstream-fail"),
        headers=BRIDGE_HEADERS,
    )
    assert failed.status_code == 502
    assert app_module.store.get_action_record("ship-upstream-fail")["status"] == "failed"

    async def successful_write(method, path, payload):
        return {"status_code": 200, "body": "ok"}

    monkeypatch.setattr(app_module, "upstream_write", successful_write)
    retried = client.post(
        "/api/orders/1234567890/create-shipping-draft",
        json=shipping_payload("ship-upstream-fail"),
        headers=BRIDGE_HEADERS,
    )
    assert retried.status_code == 200
    assert retried.json()["deduped"] is False
    assert app_module.store.get_action_record("ship-upstream-fail")["status"] == "succeeded"


def test_order_api_base_url_uses_order_client_adapter(client, monkeypatch):
    class FakeOrderClient:
        async def get_order(self, tid, plain=True):
            assert tid == "real-normal"
            assert plain is True
            return {
                "tid": "real-normal",
                "platform": "taobao",
                "shop_id": "shop_staging_001",
                "status": "WAIT_SELLER_SEND_GOODS",
                "payment": "199.00",
                "buyer": {
                    "receiver_name": "测试收件人",
                    "receiver_mobile": "13800000000",
                    "receiver_state": "浙江省",
                    "receiver_city": "杭州市",
                    "receiver_district": "西湖区",
                    "receiver_address": "测试街道1号",
                },
                "orders": [
                    {
                        "oid": "real-normal-1",
                        "sku_id": "sku_staging_001",
                        "refund_status": "NO_REFUND",
                        "inventory_status": "RESERVED",
                    }
                ],
            }

    monkeypatch.setattr(app_module, "ORDER_API_BASE_URL", "https://orders.example.test")
    monkeypatch.setattr(app_module, "create_order_api_client", lambda: FakeOrderClient())
    resp = client.get("/api/orders/real-normal?plain=true", headers=BRIDGE_HEADERS)
    assert resp.status_code == 200
    assert resp.json()["shop_id"] == "shop_staging_001"


def test_high_risk_endpoints_are_forbidden_with_auth(client):
    for action in ["ship", "refund", "close", "modify-address", "modify-price"]:
        resp = client.post(f"/api/orders/1234567890/{action}", headers=BRIDGE_HEADERS)
        assert resp.status_code == 403
