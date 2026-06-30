import asyncio
import json
from pathlib import Path

import httpx
import pytest

from order_client import (
    OrderApiClient,
    OrderApiClientConfig,
    UpstreamOrderAPIError,
    normalize_order,
    normalize_taobao_cli_payload,
)


SAMPLES = Path(__file__).resolve().parents[1] / "samples"


def load_sample(name: str) -> dict:
    return json.loads((SAMPLES / name).read_text(encoding="utf-8"))


def make_client(handler) -> OrderApiClient:
    return OrderApiClient(
        OrderApiClientConfig(
            base_url="https://orders.example.test",
            token="staging-token",
            get_order_path="/v1/trades/{tid}",
        ),
        transport=httpx.MockTransport(handler),
    )


def test_normalize_order_supports_staging_aliases():
    normalized = normalize_order(load_sample("real_order_normal.json"))
    assert normalized["tid"] == "900000000001"
    assert normalized["shop_id"] == "shop_staging_001"
    assert normalized["status"] == "WAIT_SELLER_SEND_GOODS"
    assert normalized["buyer"]["receiver_mobile"] == "13800000000"
    assert normalized["buyer"]["receiver_state"] == "浙江省"
    assert normalized["orders"][0]["oid"] == "900000000001-1"
    assert normalized["orders"][0]["outer_sku_id"] == "ERP_SKU_STAGING_001"
    assert normalized["orders"][0]["refund_status"] == "NO_REFUND"


def test_normalize_taobao_cli_payload_maps_compact_order():
    payload = {
        "orders": [
            {
                "order_id": "3306670536474051657",
                "buyer_nick": "tb_user_xxx",
                "create_time": "2026-06-30 10:00:00",
                "status": "等待卖家发货",
                "actual_fee": "199.00",
                "items": [
                    {
                        "sub_order_id": "3306670536474051657-1",
                        "title": "商品A",
                        "sku": ["颜色:黑色", "尺码:L"],
                        "sku_id": "sku_001",
                        "quantity": 1,
                        "price": "199.00",
                    }
                ],
            }
        ],
        "raw": {
            "mainOrders": [
                {
                    "id": "3306670536474051657",
                    "buyer": {"nick": "tb_user_xxx"},
                    "receiverName": "张三",
                    "receiverMobile": "13800000000",
                    "receiverState": "浙江省",
                    "receiverCity": "杭州市",
                    "receiverDistrict": "西湖区",
                    "receiverAddress": "某某街道1号",
                }
            ]
        },
    }

    normalized = normalize_order(normalize_taobao_cli_payload(payload, "3306670536474051657"))
    assert normalized["tid"] == "3306670536474051657"
    assert normalized["status"] == "WAIT_SELLER_SEND_GOODS"
    assert normalized["buyer"]["receiver_mobile"] == "13800000000"
    assert normalized["buyer"]["receiver_state"] == "浙江省"
    assert normalized["orders"][0]["sku_properties_name"] == "颜色:黑色;尺码:L"


def test_taobao_cli_adapter_is_read_only_for_write_actions():
    client = OrderApiClient(
        OrderApiClientConfig(
            adapter="taobao_cli",
            base_url="",
            taobao_cli_repo_path="/tmp/unused",
            taobao_cli_cookie_file="/tmp/unused/cookies.txt",
        )
    )
    with pytest.raises(UpstreamOrderAPIError) as excinfo:
        asyncio.run(client.create_shipping_draft("3306670536474051657", {"idempotency_key": "idem"}))
    assert excinfo.value.status_code == 501
    assert "read-only" in str(excinfo.value.detail)


@pytest.mark.parametrize(
    ("sample_name", "status_value"),
    [
        ("real_order_refund.json", "WAIT_SELLER_AGREE"),
        ("real_order_address_abnormal.json", "NO_REFUND"),
        ("real_order_forbid.json", "NO_REFUND"),
        ("real_order_closed.json", "NO_REFUND"),
    ],
)
def test_normalize_order_samples_are_valid(sample_name, status_value):
    normalized = normalize_order(load_sample(sample_name))
    assert normalized["tid"]
    assert normalized["status"]
    assert normalized["orders"][0]["refund_status"] == status_value


def test_get_order_sends_auth_and_normalizes_response():
    seen = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["url"] = str(request.url)
        seen["authorization"] = request.headers.get("Authorization")
        return httpx.Response(200, json=load_sample("real_order_normal.json"))

    result = asyncio.run(make_client(handler).get_order("900000000001", plain=True))
    assert result["tid"] == "900000000001"
    assert "plain=true" in seen["url"]
    assert seen["authorization"] == "Bearer staging-token"


@pytest.mark.parametrize(
    ("status_code", "expected_status", "expected_detail"),
    [
        (401, 502, "order API upstream auth failed"),
        (403, 502, "order API upstream auth failed"),
        (404, 404, "order not found"),
        (500, 502, "order API returned non-2xx"),
    ],
)
def test_get_order_maps_upstream_errors(status_code, expected_status, expected_detail):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code, text="upstream problem")

    with pytest.raises(UpstreamOrderAPIError) as excinfo:
        asyncio.run(make_client(handler).get_order("900000000001", plain=True))
    assert excinfo.value.status_code == expected_status
    assert expected_detail in str(excinfo.value.detail)


def test_get_order_maps_timeout():
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.TimeoutException("slow upstream", request=request)

    with pytest.raises(UpstreamOrderAPIError) as excinfo:
        asyncio.run(make_client(handler).get_order("900000000001", plain=True))
    assert excinfo.value.status_code == 504
    assert excinfo.value.detail == "order API timeout"


def test_get_order_maps_normalization_failure():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"tid": "900000000001", "status": "WAIT_SELLER_SEND_GOODS", "orders": []})

    with pytest.raises(UpstreamOrderAPIError) as excinfo:
        asyncio.run(make_client(handler).get_order("900000000001", plain=True))
    assert excinfo.value.status_code == 502
    assert "order API normalization failed" in str(excinfo.value.detail)


def test_write_action_success_and_failure_mapping():
    calls = []

    def success_handler(request: httpx.Request) -> httpx.Response:
        calls.append((request.method, str(request.url), json.loads(request.content.decode("utf-8"))))
        return httpx.Response(200, text="ok")

    payload = {"idempotency_key": "idem-1", "note": "safe note", "actor": "taobao-order-ops"}
    success = asyncio.run(make_client(success_handler).add_internal_note("900000000001", payload))
    assert success == {"status_code": 200, "body": "ok"}
    assert calls[0][0] == "POST"
    assert calls[0][1].endswith("/api/orders/900000000001/internal-note")
    assert calls[0][2]["idempotency_key"] == "idem-1"

    def failure_handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, text="write failed")

    with pytest.raises(UpstreamOrderAPIError) as excinfo:
        asyncio.run(make_client(failure_handler).create_shipping_draft("900000000001", payload))
    assert excinfo.value.status_code == 502
    assert "order API write returned non-2xx" in str(excinfo.value.detail)
