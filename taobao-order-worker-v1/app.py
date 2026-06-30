from __future__ import annotations

import hashlib
import json
import os
import re
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Awaitable, Callable, Optional

import httpx
from fastapi import Depends, FastAPI, Header, HTTPException, status
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field


def env_bool(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def env_csv(name: str, default: str) -> list[str]:
    value = os.getenv(name, default)
    return [item.strip() for item in value.split(",") if item.strip()]


APP_NAME = os.getenv("APP_NAME", "taobao-order-worker-v1")
ENV = os.getenv("ENV", "dev")
TAOBAO_EVENT_SECRET = os.getenv("TAOBAO_EVENT_SECRET", "dev-secret")
MULTICA_AUTOPILOT_WEBHOOK_URL = os.getenv("MULTICA_AUTOPILOT_WEBHOOK_URL", "")
ORDER_BRIDGE_API_TOKEN = os.getenv("ORDER_BRIDGE_API_TOKEN") or "dev-bridge-token"
REQUIRE_ORDER_BRIDGE_API_AUTH = env_bool("REQUIRE_ORDER_BRIDGE_API_AUTH", True)
CORS_ALLOW_ORIGINS = env_csv("CORS_ALLOW_ORIGINS", "http://localhost:3000,http://localhost:8080")
ORDER_API_BASE_URL = os.getenv("ORDER_API_BASE_URL", "").rstrip("/")
ORDER_API_TOKEN = os.getenv("ORDER_API_TOKEN", "")
ORDER_API_GET_ORDER_PATH = os.getenv("ORDER_API_GET_ORDER_PATH", "/api/orders/{tid}")
ORDER_API_TIMEOUT_SECONDS = float(os.getenv("ORDER_API_TIMEOUT_SECONDS", "10"))
ORDER_API_WRITE_THROUGH = env_bool("ORDER_API_WRITE_THROUGH", False)
DEDUPE_DB_PATH = os.getenv("DEDUPE_DB_PATH", "./data/order_worker.sqlite3")
ALLOW_PLAIN_RECEIVER_INFO = env_bool("ALLOW_PLAIN_RECEIVER_INFO", True)
ALLOW_HIGH_RISK_ACTIONS = env_bool("ALLOW_HIGH_RISK_ACTIONS", False)
REMOTE_AREA_KEYWORDS = env_csv("REMOTE_AREA_KEYWORDS", "新疆,西藏,内蒙古,青海,宁夏,甘肃")
UNSUPPORTED_AREA_KEYWORDS = env_csv("UNSUPPORTED_AREA_KEYWORDS", "香港,澳门,台湾,海外")
STORE_PLAIN_RECEIVER_IN_ACTION_LOG = env_bool("STORE_PLAIN_RECEIVER_IN_ACTION_LOG", False)

if ENV != "dev":
    if not TAOBAO_EVENT_SECRET or TAOBAO_EVENT_SECRET == "dev-secret":
        raise RuntimeError("TAOBAO_EVENT_SECRET must be set to a non-dev value outside dev")
    if REQUIRE_ORDER_BRIDGE_API_AUTH and (
        not ORDER_BRIDGE_API_TOKEN or ORDER_BRIDGE_API_TOKEN == "dev-bridge-token"
    ):
        raise RuntimeError("ORDER_BRIDGE_API_TOKEN must be set to a non-dev value outside dev")


app = FastAPI(title=APP_NAME, version="1.0.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=CORS_ALLOW_ORIGINS,
    allow_credentials=False,
    allow_methods=["*"],
    allow_headers=["*"],
)


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


class Store:
    def __init__(self, db_path: str) -> None:
        self.db_path = Path(db_path)
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._init()

    def connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        return conn

    def _init(self) -> None:
        with self.connect() as conn:
            conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS processed_events (
                    event_id TEXT PRIMARY KEY,
                    tid TEXT NOT NULL,
                    idempotency_key TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    result_json TEXT NOT NULL DEFAULT '{}',
                    status TEXT NOT NULL DEFAULT 'pending',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS actions (
                    idempotency_key TEXT PRIMARY KEY,
                    tid TEXT NOT NULL,
                    action TEXT NOT NULL,
                    actor TEXT NOT NULL,
                    result_json TEXT NOT NULL DEFAULT '{}',
                    status TEXT NOT NULL DEFAULT 'pending',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS audit_log (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    actor TEXT NOT NULL,
                    action TEXT NOT NULL,
                    tid TEXT,
                    payload_json TEXT NOT NULL,
                    created_at TEXT NOT NULL
                );
                """
            )
            self._ensure_column(conn, "processed_events", "result_json", "TEXT NOT NULL DEFAULT '{}'")
            self._ensure_column(conn, "processed_events", "status", "TEXT NOT NULL DEFAULT 'sent'")
            self._ensure_column(conn, "processed_events", "updated_at", "TEXT")
            self._ensure_column(conn, "actions", "status", "TEXT NOT NULL DEFAULT 'succeeded'")
            self._ensure_column(conn, "actions", "updated_at", "TEXT")

    @staticmethod
    def _ensure_column(conn: sqlite3.Connection, table: str, column: str, definition: str) -> None:
        existing = {row["name"] for row in conn.execute(f"PRAGMA table_info({table})").fetchall()}
        if column not in existing:
            conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {definition}")

    def get_event(self, event_id: str) -> Optional[dict[str, Any]]:
        with self.connect() as conn:
            row = conn.execute(
                """
                SELECT event_id, tid, idempotency_key, payload_json, result_json, status, created_at, updated_at
                FROM processed_events
                WHERE event_id = ?
                """,
                (event_id,),
            ).fetchone()
        if row is None:
            return None
        return {
            "event_id": row["event_id"],
            "tid": row["tid"],
            "idempotency_key": row["idempotency_key"],
            "payload": json.loads(row["payload_json"]),
            "result": json.loads(row["result_json"] or "{}"),
            "status": row["status"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
        }

    def record_event_status(
        self,
        event_id: str,
        tid: str,
        idempotency_key: str,
        payload: dict[str, Any],
        event_status: str,
        result: Optional[dict[str, Any]] = None,
    ) -> None:
        now = utc_now()
        with self.connect() as conn:
            conn.execute(
                """
                INSERT INTO processed_events(
                    event_id, tid, idempotency_key, payload_json, result_json, status, created_at, updated_at
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(event_id) DO UPDATE SET
                    tid = excluded.tid,
                    idempotency_key = excluded.idempotency_key,
                    payload_json = excluded.payload_json,
                    result_json = excluded.result_json,
                    status = excluded.status,
                    updated_at = excluded.updated_at
                """,
                (
                    event_id,
                    tid,
                    idempotency_key,
                    json.dumps(payload, ensure_ascii=False),
                    json.dumps(result or {}, ensure_ascii=False),
                    event_status,
                    now,
                    now,
                ),
            )

    def get_action_record(self, idempotency_key: str) -> Optional[dict[str, Any]]:
        with self.connect() as conn:
            row = conn.execute(
                """
                SELECT idempotency_key, tid, action, actor, result_json, status, created_at, updated_at
                FROM actions
                WHERE idempotency_key = ?
                """,
                (idempotency_key,),
            ).fetchone()
        if row is None:
            return None
        return {
            "idempotency_key": row["idempotency_key"],
            "tid": row["tid"],
            "action": row["action"],
            "actor": row["actor"],
            "result": json.loads(row["result_json"] or "{}"),
            "status": row["status"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
        }

    def reserve_action(
        self,
        tid: str,
        action: str,
        idempotency_key: str,
        actor: str,
    ) -> tuple[str, Optional[dict[str, Any]]]:
        now = utc_now()
        try:
            with self.connect() as conn:
                conn.execute(
                    """
                    INSERT INTO actions(idempotency_key, tid, action, actor, result_json, status, created_at, updated_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (idempotency_key, tid, action, actor, "{}", "pending", now, now),
                )
            return "reserved", None
        except sqlite3.IntegrityError:
            existing = self.get_action_record(idempotency_key)
            if existing is None:
                return "pending", None
            if existing["status"] == "succeeded":
                return "succeeded", existing["result"]
            if existing["status"] == "pending":
                return "pending", existing["result"]

            with self.connect() as conn:
                conn.execute(
                    """
                    UPDATE actions
                    SET tid = ?, action = ?, actor = ?, result_json = '{}', status = 'pending', updated_at = ?
                    WHERE idempotency_key = ?
                    """,
                    (tid, action, actor, utc_now(), idempotency_key),
                )
            return "reserved", None

    def finish_action(self, idempotency_key: str, result: dict[str, Any]) -> None:
        with self.connect() as conn:
            conn.execute(
                """
                UPDATE actions
                SET result_json = ?, status = 'succeeded', updated_at = ?
                WHERE idempotency_key = ?
                """,
                (json.dumps(result, ensure_ascii=False), utc_now(), idempotency_key),
            )

    def fail_action(self, idempotency_key: str, result: dict[str, Any]) -> None:
        with self.connect() as conn:
            conn.execute(
                """
                UPDATE actions
                SET result_json = ?, status = 'failed', updated_at = ?
                WHERE idempotency_key = ?
                """,
                (json.dumps(result, ensure_ascii=False), utc_now(), idempotency_key),
            )

    def audit(self, actor: str, action: str, tid: Optional[str], payload: dict[str, Any]) -> None:
        with self.connect() as conn:
            conn.execute(
                """
                INSERT INTO audit_log(actor, action, tid, payload_json, created_at)
                VALUES (?, ?, ?, ?, ?)
                """,
                (actor, action, tid, json.dumps(payload, ensure_ascii=False), utc_now()),
            )


store = Store(DEDUPE_DB_PATH)


class TaobaoEventPayload(BaseModel):
    platform: str = "taobao"
    shop_id: str
    tid: str
    status: str
    modified: Optional[str] = None
    use_plain_receiver_info: bool = True
    idempotency_key: Optional[str] = None


class TaobaoOrderEvent(BaseModel):
    event: str = "taobao.trade.modified"
    eventId: str
    eventPayload: TaobaoEventPayload


class BuyerInfo(BaseModel):
    buyer_nick: Optional[str] = None
    buyer_open_id: Optional[str] = None
    receiver_oaid: Optional[str] = None
    receiver_name: Optional[str] = None
    receiver_mobile: Optional[str] = None
    receiver_phone: Optional[str] = None
    receiver_state: Optional[str] = None
    receiver_city: Optional[str] = None
    receiver_district: Optional[str] = None
    receiver_address: Optional[str] = None
    receiver_zip: Optional[str] = None


class OrderItem(BaseModel):
    oid: str
    sku_id: Optional[str] = None
    outer_sku_id: Optional[str] = None
    title: Optional[str] = None
    sku_properties_name: Optional[str] = None
    num: int = 1
    price: Optional[str] = None
    payment: Optional[str] = None
    refund_status: str = "NO_REFUND"
    inventory_status: str = "RESERVED"


class OrderDetail(BaseModel):
    tid: str
    platform: str = "taobao"
    shop_id: str
    status: str
    payment: Optional[str] = None
    buyer: BuyerInfo
    orders: list[OrderItem]
    buyer_message: Optional[str] = None
    seller_memo: Optional[str] = None
    created: Optional[str] = None
    modified: Optional[str] = None


class FulfillmentCheckRequest(BaseModel):
    plain: bool = True
    check_items: list[str] = Field(default_factory=list)


class ReceiverCheck(BaseModel):
    name_valid: bool
    mobile_valid: bool
    address_complete: bool
    province: Optional[str] = None
    city: Optional[str] = None
    district: Optional[str] = None
    remote_area: bool = False
    logistics_supported: bool = True


class FulfillmentCheckResponse(BaseModel):
    tid: str
    can_ship: bool
    risk_level: str
    reason_codes: list[str]
    required_action: str
    receiver_check: ReceiverCheck
    summary: str
    safe_summary: str
    metadata: dict[str, Any] = Field(default_factory=dict)


class ActionRequest(BaseModel):
    idempotency_key: str
    actor: str = "taobao-order-ops"


class InternalNoteRequest(ActionRequest):
    note: str


class TagRequest(ActionRequest):
    tag: str


class HoldRequest(ActionRequest):
    reason_code: str
    reason: str


class ShippingDraftRequest(ActionRequest):
    warehouse_id: str
    logistics_company: str
    package_note: Optional[str] = None


class ActionResponse(BaseModel):
    ok: bool
    deduped: bool = False
    tid: str
    action: str
    idempotency_key: str
    result: dict[str, Any]


async def require_bridge_token(x_order_bridge_token: Optional[str] = Header(default=None)) -> None:
    if not REQUIRE_ORDER_BRIDGE_API_AUTH:
        return
    if not ORDER_BRIDGE_API_TOKEN:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="ORDER_BRIDGE_API_TOKEN is required when bridge auth is enabled",
        )
    if x_order_bridge_token != ORDER_BRIDGE_API_TOKEN:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid order bridge token")


def mock_order(tid: str) -> OrderDetail:
    tid_lower = tid.lower()
    status_value = "WAIT_SELLER_SEND_GOODS"
    refund_status = "NO_REFUND"
    inventory_status = "RESERVED"
    sku_id: Optional[str] = "sku_001"
    outer_sku_id: Optional[str] = "ERP_SKU_001"
    receiver_name: Optional[str] = "张三"
    receiver_mobile: Optional[str] = "13888888888"
    state = "浙江省"
    city = "杭州市"
    district = "西湖区"
    address = "某某街道某某小区1幢101室"
    buyer_message = "请尽快发货"

    if "waitpay" in tid_lower:
        status_value = "WAIT_BUYER_PAY"
    if "confirm" in tid_lower:
        status_value = "WAIT_BUYER_CONFIRM_GOODS"
    if "finished" in tid_lower:
        status_value = "TRADE_FINISHED"
    if "closed-by-taobao" in tid_lower:
        status_value = "TRADE_CLOSED_BY_TAOBAO"
    elif "closed" in tid_lower:
        status_value = "TRADE_CLOSED"
    if "refund" in tid_lower:
        refund_status = "WAIT_SELLER_AGREE"
    if "forbid" in tid_lower:
        status_value = "PAID_FORBID_CONSIGN"
    if "address" in tid_lower:
        district = ""
        address = "科技园"
    if "stockout" in tid_lower:
        inventory_status = "OUT_OF_STOCK"
    if "sku" in tid_lower:
        sku_id = None
        outer_sku_id = None
    if "mobile" in tid_lower:
        receiver_mobile = "12345"
    if "noname" in tid_lower:
        receiver_name = ""
    if "message" in tid_lower:
        buyer_message = "请改地址后再发货，指定顺丰"
    if "remote" in tid_lower:
        state = "新疆"
        city = "乌鲁木齐市"
        district = "天山区"
        address = "测试路1号"
    if "unsupported" in tid_lower:
        state = "香港"
        city = "香港"
        district = "中西区"
        address = "中环测试地址"

    return OrderDetail(
        tid=tid,
        platform="taobao",
        shop_id="shop_001",
        status=status_value,
        payment="199.00",
        buyer=BuyerInfo(
            buyer_nick="tb_user_xxx",
            buyer_open_id="xxx",
            receiver_oaid="xxx",
            receiver_name=receiver_name,
            receiver_mobile=receiver_mobile,
            receiver_state=state,
            receiver_city=city,
            receiver_district=district,
            receiver_address=address,
            receiver_zip="310000",
        ),
        orders=[
            OrderItem(
                oid="111",
                sku_id=sku_id,
                outer_sku_id=outer_sku_id,
                title="商品A",
                sku_properties_name="颜色:黑色;尺码:L",
                num=1,
                price="199.00",
                payment="199.00",
                refund_status=refund_status,
                inventory_status=inventory_status,
            )
        ],
        buyer_message=buyer_message,
        seller_memo="加急",
        created="2026-06-30T10:00:00+08:00",
        modified="2026-06-30T10:12:00+08:00",
    )


def upstream_body(resp: httpx.Response) -> str:
    return resp.text[:2000]


def upstream_detail(resp: httpx.Response, service: str) -> dict[str, Any]:
    return {"message": f"{service} returned non-2xx", "status_code": resp.status_code, "body": upstream_body(resp)}


async def get_order_from_source(tid: str, plain: bool) -> OrderDetail:
    if not ORDER_API_BASE_URL:
        return mock_order(tid)

    path = ORDER_API_GET_ORDER_PATH.format(tid=tid)
    headers = {"Authorization": f"Bearer {ORDER_API_TOKEN}"} if ORDER_API_TOKEN else {}
    try:
        async with httpx.AsyncClient(timeout=ORDER_API_TIMEOUT_SECONDS) as client:
            resp = await client.get(
                f"{ORDER_API_BASE_URL}{path}",
                params={"plain": str(plain).lower()},
                headers=headers,
            )
    except httpx.TimeoutException:
        raise HTTPException(status_code=status.HTTP_504_GATEWAY_TIMEOUT, detail="order API timeout") from None
    except httpx.RequestError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail={"message": "order API request failed", "error": str(exc)},
        ) from exc

    if resp.status_code == status.HTTP_404_NOT_FOUND:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="order not found")
    if resp.status_code in {status.HTTP_401_UNAUTHORIZED, status.HTTP_403_FORBIDDEN}:
        raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail="order API upstream auth failed")
    if not 200 <= resp.status_code < 300:
        raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail=upstream_detail(resp, "order API"))
    return OrderDetail.model_validate(resp.json())


async def post_multica_autopilot(payload: dict[str, Any]) -> dict[str, Any]:
    if not MULTICA_AUTOPILOT_WEBHOOK_URL:
        return {"skipped": True, "reason": "MULTICA_AUTOPILOT_WEBHOOK_URL is empty", "payload": payload}
    try:
        async with httpx.AsyncClient(timeout=ORDER_API_TIMEOUT_SECONDS) as client:
            resp = await client.post(MULTICA_AUTOPILOT_WEBHOOK_URL, json=payload)
    except httpx.TimeoutException:
        raise HTTPException(status_code=status.HTTP_504_GATEWAY_TIMEOUT, detail="Multica webhook timeout") from None
    except httpx.RequestError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail={"message": "Multica webhook request failed", "error": str(exc)},
        ) from exc
    if not 200 <= resp.status_code < 300:
        raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail=upstream_detail(resp, "Multica webhook"))
    return {"status_code": resp.status_code, "body": upstream_body(resp)}


async def upstream_write(method: str, path: str, payload: dict[str, Any]) -> dict[str, Any]:
    if not ORDER_API_WRITE_THROUGH or not ORDER_API_BASE_URL:
        return {"skipped": True, "reason": "ORDER_API_WRITE_THROUGH is false or ORDER_API_BASE_URL is empty"}
    headers = {"Authorization": f"Bearer {ORDER_API_TOKEN}"} if ORDER_API_TOKEN else {}
    try:
        async with httpx.AsyncClient(timeout=ORDER_API_TIMEOUT_SECONDS) as client:
            resp = await client.request(method, f"{ORDER_API_BASE_URL}{path}", json=payload, headers=headers)
    except httpx.TimeoutException:
        raise HTTPException(status_code=status.HTTP_504_GATEWAY_TIMEOUT, detail="order API write timeout") from None
    except httpx.RequestError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail={"message": "order API write request failed", "error": str(exc)},
        ) from exc
    if not 200 <= resp.status_code < 300:
        raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail=upstream_detail(resp, "order API write"))
    return {"status_code": resp.status_code, "body": upstream_body(resp)}


def validate_mobile(phone: Optional[str]) -> bool:
    return bool(phone and re.match(r"^1[3-9]\d{9}$", phone))


def contains_keyword(values: list[Optional[str]], keywords: list[str]) -> bool:
    text = " ".join(value or "" for value in values)
    return any(keyword and keyword in text for keyword in keywords)


def receiver_check_for(buyer: BuyerInfo) -> ReceiverCheck:
    location_parts = [
        buyer.receiver_state,
        buyer.receiver_city,
        buyer.receiver_district,
        buyer.receiver_address,
    ]
    unsupported_area = contains_keyword(location_parts, UNSUPPORTED_AREA_KEYWORDS)
    remote_area = contains_keyword(location_parts, REMOTE_AREA_KEYWORDS)
    return ReceiverCheck(
        name_valid=bool(buyer.receiver_name),
        mobile_valid=validate_mobile(buyer.receiver_mobile),
        address_complete=bool(
            buyer.receiver_state and buyer.receiver_city and buyer.receiver_district and buyer.receiver_address
        ),
        province=buyer.receiver_state,
        city=buyer.receiver_city,
        district=buyer.receiver_district,
        remote_area=remote_area,
        logistics_supported=not unsupported_area,
    )


def fulfillment_response(
    order: OrderDetail,
    receiver_check: ReceiverCheck,
    can_ship: bool,
    risk_level: str,
    reason_codes: list[str],
    required_action: str,
    safe_summary: str,
    fulfillment_status: str,
) -> FulfillmentCheckResponse:
    return FulfillmentCheckResponse(
        tid=order.tid,
        can_ship=can_ship,
        risk_level=risk_level,
        reason_codes=reason_codes,
        required_action=required_action,
        receiver_check=receiver_check,
        summary=safe_summary,
        safe_summary=safe_summary,
        metadata={
            "shop_id": order.shop_id,
            "order_status": order.status,
            "fulfillment_status": fulfillment_status,
            "checked_at": utc_now(),
        },
    )


def evaluate_order(order: OrderDetail) -> FulfillmentCheckResponse:
    buyer = order.buyer
    receiver_check = receiver_check_for(buyer)
    ignore_statuses = {
        "WAIT_BUYER_PAY",
        "WAIT_BUYER_CONFIRM_GOODS",
        "TRADE_FINISHED",
        "TRADE_CLOSED",
        "TRADE_CLOSED_BY_TAOBAO",
    }

    if order.status in ignore_statuses:
        return fulfillment_response(
            order=order,
            receiver_check=receiver_check,
            can_ship=False,
            risk_level="low",
            reason_codes=[f"STATUS_{order.status}"],
            required_action="ignore",
            safe_summary=f"订单状态为 {order.status}，不进入发货处理。",
            fulfillment_status="ignored",
        )

    if order.status == "PAID_FORBID_CONSIGN":
        return fulfillment_response(
            order=order,
            receiver_check=receiver_check,
            can_ship=False,
            risk_level="high",
            reason_codes=["PAID_FORBID_CONSIGN"],
            required_action="hold",
            safe_summary="订单被平台标记为禁止发货，必须挂起并等待人工处理。",
            fulfillment_status="blocked",
        )

    if order.status != "WAIT_SELLER_SEND_GOODS":
        return fulfillment_response(
            order=order,
            receiver_check=receiver_check,
            can_ship=False,
            risk_level="medium",
            reason_codes=["ORDER_NOT_WAITING_SHIPMENT"],
            required_action="manual_review",
            safe_summary=f"订单状态为 {order.status}，不是待发货状态，需要人工确认。",
            fulfillment_status="manual_review",
        )

    reason_codes: list[str] = []
    if any(item.refund_status != "NO_REFUND" for item in order.orders):
        reason_codes.append("REFUND_IN_PROGRESS")
    if any(item.inventory_status != "RESERVED" for item in order.orders):
        reason_codes.append("INVENTORY_NOT_RESERVED")
    if any(not item.sku_id and not item.outer_sku_id for item in order.orders):
        reason_codes.append("SKU_MAPPING_MISSING")
    if not receiver_check.name_valid:
        reason_codes.append("RECEIVER_NAME_MISSING")
    if not receiver_check.mobile_valid:
        reason_codes.append("RECEIVER_MOBILE_INVALID")
    if not receiver_check.address_complete:
        reason_codes.append("ADDRESS_INCOMPLETE")
    if not receiver_check.logistics_supported:
        reason_codes.append("LOGISTICS_UNSUPPORTED_AREA")
    if receiver_check.remote_area:
        reason_codes.append("REMOTE_AREA_REQUIRES_REVIEW")
    message_keywords = ["改地址", "修改地址", "不要发", "别发", "退款", "指定快递", "顺丰", "拆单", "合单", "赠品", "发票"]
    if contains_keyword([order.buyer_message, order.seller_memo], message_keywords):
        reason_codes.append("MESSAGE_REQUIRES_MANUAL_REVIEW")

    if not reason_codes:
        return fulfillment_response(
            order=order,
            receiver_check=receiver_check,
            can_ship=True,
            risk_level="low",
            reason_codes=[],
            required_action="create_shipping_draft",
            safe_summary="订单为待发货状态，未发现退款、禁止发货、库存、SKU、地址、留言或物流异常，可创建发货草稿。",
            fulfillment_status="shipping_draft_ready",
        )

    high_risk_codes = {
        "PAID_FORBID_CONSIGN",
        "REFUND_IN_PROGRESS",
        "INVENTORY_NOT_RESERVED",
        "LOGISTICS_UNSUPPORTED_AREA",
    }
    required_action = "hold" if any(code in high_risk_codes for code in reason_codes) else "manual_review"
    risk_level = "high" if required_action == "hold" else "medium"
    fulfillment_status = "blocked" if required_action == "hold" else "manual_review"
    return fulfillment_response(
        order=order,
        receiver_check=receiver_check,
        can_ship=False,
        risk_level=risk_level,
        reason_codes=reason_codes,
        required_action=required_action,
        safe_summary=f"订单未通过发货前检查：{', '.join(reason_codes)}。",
        fulfillment_status=fulfillment_status,
    )


def deny_high_risk(action: str) -> dict[str, Any]:
    if ALLOW_HIGH_RISK_ACTIONS:
        return {"ok": False, "denied": True, "action": action, "reason": "high-risk action handler is not implemented"}
    return {
        "ok": False,
        "denied": True,
        "action": action,
        "reason": "1.0 forbids direct high-risk actions; use safe actions only",
    }


async def idempotent_action(
    tid: str,
    action: str,
    idempotency_key: str,
    actor: str,
    execute: Callable[[], Awaitable[dict[str, Any]]],
) -> ActionResponse:
    reservation, existing = store.reserve_action(tid, action, idempotency_key, actor)
    if reservation == "succeeded" and existing is not None:
        return ActionResponse(
            ok=True,
            deduped=True,
            tid=tid,
            action=action,
            idempotency_key=idempotency_key,
            result=existing,
        )
    if reservation == "pending":
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="action is already being processed")

    try:
        result = await execute()
    except HTTPException as exc:
        store.fail_action(idempotency_key, {"error": exc.detail, "status_code": exc.status_code})
        raise
    except Exception as exc:
        store.fail_action(idempotency_key, {"error": str(exc), "status_code": 502})
        raise

    store.finish_action(idempotency_key, result)
    store.audit(actor=actor, action=action, tid=tid, payload=result)
    return ActionResponse(ok=True, tid=tid, action=action, idempotency_key=idempotency_key, result=result)


@app.get("/health")
async def health() -> dict[str, Any]:
    return {"ok": True, "app": APP_NAME, "env": ENV, "time": utc_now()}


@app.post("/taobao/order-event")
async def receive_taobao_order_event(
    event: TaobaoOrderEvent,
    x_order_event_secret: Optional[str] = Header(default=None),
) -> dict[str, Any]:
    if x_order_event_secret != TAOBAO_EVENT_SECRET:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid order event secret")

    payload = event.eventPayload
    tid = payload.tid
    idempotency_key = payload.idempotency_key or hashlib.sha256(event.eventId.encode("utf-8")).hexdigest()
    existing = store.get_event(event.eventId)
    if existing and existing["status"] == "sent":
        return {
            "ok": True,
            "deduped": True,
            "event_id": event.eventId,
            "idempotency_key": idempotency_key,
            "multica": existing["result"],
        }
    if existing and existing["status"] == "pending":
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="event is already being processed")

    event_payload = event.model_dump(mode="json")
    store.record_event_status(event.eventId, tid, idempotency_key, event_payload, "pending")
    multica_payload = {
        "event": event.event,
        "eventId": event.eventId,
        "eventPayload": {
            "platform": payload.platform,
            "shop_id": payload.shop_id,
            "tid": tid,
            "status": payload.status,
            "modified": payload.modified,
            "use_plain_receiver_info": payload.use_plain_receiver_info,
            "idempotency_key": idempotency_key,
        },
    }

    try:
        multica_result = await post_multica_autopilot(multica_payload)
    except HTTPException as exc:
        failure = {"error": exc.detail, "status_code": exc.status_code}
        store.record_event_status(event.eventId, tid, idempotency_key, event_payload, "failed", failure)
        store.audit(actor="order-bridge", action="trigger_multica_autopilot_failed", tid=tid, payload=failure)
        raise

    store.record_event_status(event.eventId, tid, idempotency_key, event_payload, "sent", multica_result)
    store.audit(
        actor="order-bridge",
        action="trigger_multica_autopilot",
        tid=tid,
        payload={"event_id": event.eventId, "result": multica_result},
    )
    return {
        "ok": True,
        "deduped": False,
        "event_id": event.eventId,
        "idempotency_key": idempotency_key,
        "multica": multica_result,
    }


@app.get("/api/orders/{tid}", dependencies=[Depends(require_bridge_token)])
async def get_order(tid: str, plain: bool = True) -> dict[str, Any]:
    if plain and not ALLOW_PLAIN_RECEIVER_INFO:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="plain receiver info is disabled")
    order = await get_order_from_source(tid, plain=plain)
    store.audit(
        actor="taobao-order-ops",
        action="get_order_plain" if plain else "get_order",
        tid=tid,
        payload={"plain": plain},
    )
    return order.model_dump(mode="json")


@app.post("/api/orders/{tid}/check-fulfillment", response_model=FulfillmentCheckResponse, dependencies=[Depends(require_bridge_token)])
async def check_fulfillment(tid: str, req: Optional[FulfillmentCheckRequest] = None) -> FulfillmentCheckResponse:
    plain = True if req is None else req.plain
    order = await get_order_from_source(tid, plain=plain)
    result = evaluate_order(order)
    store.audit(
        actor="taobao-order-ops",
        action="check_fulfillment",
        tid=tid,
        payload=result.model_dump(mode="json"),
    )
    return result


@app.post("/api/orders/{tid}/internal-note", response_model=ActionResponse, dependencies=[Depends(require_bridge_token)])
async def add_internal_note(tid: str, req: InternalNoteRequest) -> ActionResponse:
    async def execute() -> dict[str, Any]:
        local = {"saved": True, "type": "internal_note", "tid": tid, "note": req.note, "actor": req.actor}
        upstream = await upstream_write("POST", f"/api/orders/{tid}/internal-note", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "internal_note", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/tag", response_model=ActionResponse, dependencies=[Depends(require_bridge_token)])
async def add_tag(tid: str, req: TagRequest) -> ActionResponse:
    async def execute() -> dict[str, Any]:
        local = {"saved": True, "type": "tag", "tid": tid, "tag": req.tag, "actor": req.actor}
        upstream = await upstream_write("POST", f"/api/orders/{tid}/tag", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "tag", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/hold", response_model=ActionResponse, dependencies=[Depends(require_bridge_token)])
async def hold_order(tid: str, req: HoldRequest) -> ActionResponse:
    async def execute() -> dict[str, Any]:
        local = {
            "saved": True,
            "type": "hold",
            "tid": tid,
            "reason_code": req.reason_code,
            "reason": req.reason,
            "actor": req.actor,
        }
        upstream = await upstream_write("POST", f"/api/orders/{tid}/hold", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "hold", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/create-shipping-draft", response_model=ActionResponse, dependencies=[Depends(require_bridge_token)])
async def create_shipping_draft(tid: str, req: ShippingDraftRequest) -> ActionResponse:
    async def execute() -> dict[str, Any]:
        order = await get_order_from_source(tid, plain=True)
        check = evaluate_order(order)
        if not check.can_ship:
            raise HTTPException(
                status_code=status.HTTP_409_CONFLICT,
                detail={
                    "message": "fulfillment check failed; shipping draft not created",
                    "check": check.model_dump(mode="json"),
                },
            )

        receiver_summary = {
            "receiver_present": bool(order.buyer.receiver_name or order.buyer.receiver_mobile or order.buyer.receiver_address),
            "province": check.receiver_check.province,
            "city": check.receiver_check.city,
            "district": check.receiver_check.district,
            "remote_area": check.receiver_check.remote_area,
            "logistics_supported": check.receiver_check.logistics_supported,
        }
        local: dict[str, Any] = {
            "saved": True,
            "type": "shipping_draft",
            "tid": tid,
            "warehouse_id": req.warehouse_id,
            "logistics_company": req.logistics_company,
            "package_note": req.package_note,
            "actor": req.actor,
            "receiver_summary": receiver_summary,
            "items": [item.model_dump(mode="json") for item in order.orders],
        }
        if STORE_PLAIN_RECEIVER_IN_ACTION_LOG:
            local["receiver"] = order.buyer.model_dump(mode="json")

        upstream = await upstream_write("POST", f"/api/orders/{tid}/create-shipping-draft", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "create_shipping_draft", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/ship", dependencies=[Depends(require_bridge_token)])
async def ship_order(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("ship"))


@app.post("/api/orders/{tid}/refund", dependencies=[Depends(require_bridge_token)])
async def refund_order(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("refund"))


@app.post("/api/orders/{tid}/close", dependencies=[Depends(require_bridge_token)])
async def close_order(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("close"))


@app.post("/api/orders/{tid}/modify-address", dependencies=[Depends(require_bridge_token)])
async def modify_address(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("modify-address"))


@app.post("/api/orders/{tid}/modify-price", dependencies=[Depends(require_bridge_token)])
async def modify_price(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("modify-price"))
