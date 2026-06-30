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
from fastapi import FastAPI, Header, HTTPException, status
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field


APP_NAME = os.getenv("APP_NAME", "taobao-order-worker-v1")
ENV = os.getenv("ENV", "dev")
TAOBAO_EVENT_SECRET = os.getenv("TAOBAO_EVENT_SECRET", "dev-secret")
MULTICA_AUTOPILOT_WEBHOOK_URL = os.getenv("MULTICA_AUTOPILOT_WEBHOOK_URL", "")
ORDER_API_BASE_URL = os.getenv("ORDER_API_BASE_URL", "").rstrip("/")
ORDER_API_TOKEN = os.getenv("ORDER_API_TOKEN", "")
ORDER_API_GET_ORDER_PATH = os.getenv("ORDER_API_GET_ORDER_PATH", "/api/orders/{tid}")
ORDER_API_TIMEOUT_SECONDS = float(os.getenv("ORDER_API_TIMEOUT_SECONDS", "10"))
ORDER_API_WRITE_THROUGH = os.getenv("ORDER_API_WRITE_THROUGH", "false").lower() == "true"
DEDUPE_DB_PATH = os.getenv("DEDUPE_DB_PATH", "./data/order_worker.sqlite3")
ALLOW_PLAIN_RECEIVER_INFO = os.getenv("ALLOW_PLAIN_RECEIVER_INFO", "true").lower() == "true"
ALLOW_HIGH_RISK_ACTIONS = os.getenv("ALLOW_HIGH_RISK_ACTIONS", "false").lower() == "true"


app = FastAPI(title=APP_NAME, version="1.0.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
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
                    created_at TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS actions (
                    idempotency_key TEXT PRIMARY KEY,
                    tid TEXT NOT NULL,
                    action TEXT NOT NULL,
                    actor TEXT NOT NULL,
                    result_json TEXT NOT NULL,
                    created_at TEXT NOT NULL
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

    def record_event(self, event_id: str, tid: str, idempotency_key: str, payload: dict[str, Any]) -> bool:
        try:
            with self.connect() as conn:
                conn.execute(
                    """
                    INSERT INTO processed_events(event_id, tid, idempotency_key, payload_json, created_at)
                    VALUES (?, ?, ?, ?, ?)
                    """,
                    (event_id, tid, idempotency_key, json.dumps(payload, ensure_ascii=False), utc_now()),
                )
            return True
        except sqlite3.IntegrityError:
            return False

    def get_action(self, idempotency_key: str) -> Optional[dict[str, Any]]:
        with self.connect() as conn:
            row = conn.execute(
                "SELECT result_json FROM actions WHERE idempotency_key = ?",
                (idempotency_key,),
            ).fetchone()
        if row is None:
            return None
        return json.loads(row["result_json"])

    def record_action(
        self,
        tid: str,
        action: str,
        idempotency_key: str,
        actor: str,
        result: dict[str, Any],
    ) -> None:
        with self.connect() as conn:
            conn.execute(
                """
                INSERT INTO actions(idempotency_key, tid, action, actor, result_json, created_at)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                (idempotency_key, tid, action, actor, json.dumps(result, ensure_ascii=False), utc_now()),
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


def mock_order(tid: str) -> OrderDetail:
    status_value = "WAIT_SELLER_SEND_GOODS"
    refund_status = "NO_REFUND"
    inventory_status = "RESERVED"
    district = "西湖区"
    address = "某某街道某某小区1幢101室"

    if "refund" in tid:
        refund_status = "WAIT_SELLER_AGREE"
    if "forbid" in tid:
        status_value = "PAID_FORBID_CONSIGN"
    if "address" in tid:
        district = ""
        address = "科技园"
    if "stockout" in tid:
        inventory_status = "OUT_OF_STOCK"

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
            receiver_name="张三",
            receiver_mobile="13888888888",
            receiver_state="浙江省",
            receiver_city="杭州市",
            receiver_district=district,
            receiver_address=address,
            receiver_zip="310000",
        ),
        orders=[
            OrderItem(
                oid="111",
                sku_id="sku_001",
                outer_sku_id="ERP_SKU_001",
                title="商品A",
                sku_properties_name="颜色:黑色;尺码:L",
                num=1,
                price="199.00",
                payment="199.00",
                refund_status=refund_status,
                inventory_status=inventory_status,
            )
        ],
        buyer_message="请尽快发货",
        seller_memo="加急",
        created="2026-06-30T10:00:00+08:00",
        modified="2026-06-30T10:12:00+08:00",
    )


async def get_order_from_source(tid: str, plain: bool) -> OrderDetail:
    if not ORDER_API_BASE_URL:
        return mock_order(tid)

    path = ORDER_API_GET_ORDER_PATH.format(tid=tid)
    headers = {"Authorization": f"Bearer {ORDER_API_TOKEN}"} if ORDER_API_TOKEN else {}
    async with httpx.AsyncClient(timeout=ORDER_API_TIMEOUT_SECONDS) as client:
        resp = await client.get(f"{ORDER_API_BASE_URL}{path}", params={"plain": str(plain).lower()}, headers=headers)
        resp.raise_for_status()
        return OrderDetail.model_validate(resp.json())


async def post_multica_autopilot(payload: dict[str, Any]) -> dict[str, Any]:
    if not MULTICA_AUTOPILOT_WEBHOOK_URL:
        return {"skipped": True, "reason": "MULTICA_AUTOPILOT_WEBHOOK_URL is empty", "payload": payload}
    async with httpx.AsyncClient(timeout=ORDER_API_TIMEOUT_SECONDS) as client:
        resp = await client.post(MULTICA_AUTOPILOT_WEBHOOK_URL, json=payload)
        return {"status_code": resp.status_code, "body": resp.text[:2000]}


async def upstream_write(method: str, path: str, payload: dict[str, Any]) -> dict[str, Any]:
    if not ORDER_API_WRITE_THROUGH or not ORDER_API_BASE_URL:
        return {"skipped": True, "reason": "ORDER_API_WRITE_THROUGH is false or ORDER_API_BASE_URL is empty"}
    headers = {"Authorization": f"Bearer {ORDER_API_TOKEN}"} if ORDER_API_TOKEN else {}
    async with httpx.AsyncClient(timeout=ORDER_API_TIMEOUT_SECONDS) as client:
        resp = await client.request(method, f"{ORDER_API_BASE_URL}{path}", json=payload, headers=headers)
        return {"status_code": resp.status_code, "body": resp.text[:2000]}


def validate_mobile(phone: Optional[str]) -> bool:
    return bool(phone and re.match(r"^1[3-9]\d{9}$", phone))


def evaluate_order(order: OrderDetail) -> FulfillmentCheckResponse:
    reason_codes: list[str] = []
    buyer = order.buyer

    receiver_check = ReceiverCheck(
        name_valid=bool(buyer.receiver_name),
        mobile_valid=validate_mobile(buyer.receiver_mobile),
        address_complete=bool(
            buyer.receiver_state and buyer.receiver_city and buyer.receiver_district and buyer.receiver_address
        ),
        province=buyer.receiver_state,
        city=buyer.receiver_city,
        district=buyer.receiver_district,
        remote_area=False,
        logistics_supported=True,
    )

    if order.status != "WAIT_SELLER_SEND_GOODS":
        reason_codes.append("PAID_FORBID_CONSIGN" if order.status == "PAID_FORBID_CONSIGN" else "ORDER_NOT_WAITING_SHIPMENT")
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
        reason_codes.append("LOGISTICS_UNSUPPORTED")

    if not reason_codes:
        return FulfillmentCheckResponse(
            tid=order.tid,
            can_ship=True,
            risk_level="low",
            reason_codes=[],
            required_action="create_shipping_draft",
            receiver_check=receiver_check,
            summary="订单为待发货状态，未发现退款、禁止发货、库存、SKU、地址或物流异常，可创建发货草稿。",
        )

    high_risk_codes = {"PAID_FORBID_CONSIGN", "REFUND_IN_PROGRESS", "INVENTORY_NOT_RESERVED"}
    required_action = "hold" if any(code in high_risk_codes for code in reason_codes) else "manual_review"
    risk_level = "high" if required_action == "hold" else "medium"
    return FulfillmentCheckResponse(
        tid=order.tid,
        can_ship=False,
        risk_level=risk_level,
        reason_codes=reason_codes,
        required_action=required_action,
        receiver_check=receiver_check,
        summary=f"订单未通过发货前检查：{', '.join(reason_codes)}。",
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
    existing = store.get_action(idempotency_key)
    if existing is not None:
        return ActionResponse(
            ok=True,
            deduped=True,
            tid=tid,
            action=action,
            idempotency_key=idempotency_key,
            result=existing,
        )

    result = await execute()
    store.record_action(tid=tid, action=action, idempotency_key=idempotency_key, actor=actor, result=result)
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
    inserted = store.record_event(event.eventId, tid, idempotency_key, event.model_dump(mode="json"))
    if not inserted:
        return {"ok": True, "deduped": True, "event_id": event.eventId, "idempotency_key": idempotency_key}

    multica_payload = {
        "event": event.event,
        "eventId": event.eventId,
        "trigger_payload": {
            "platform": payload.platform,
            "shop_id": payload.shop_id,
            "tid": tid,
            "status": payload.status,
            "modified": payload.modified,
            "use_plain_receiver_info": payload.use_plain_receiver_info,
            "idempotency_key": idempotency_key,
        },
    }
    multica_result = await post_multica_autopilot(multica_payload)
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


@app.get("/api/orders/{tid}")
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


@app.post("/api/orders/{tid}/check-fulfillment", response_model=FulfillmentCheckResponse)
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


@app.post("/api/orders/{tid}/internal-note", response_model=ActionResponse)
async def add_internal_note(tid: str, req: InternalNoteRequest) -> ActionResponse:
    async def execute() -> dict[str, Any]:
        local = {"saved": True, "type": "internal_note", "tid": tid, "note": req.note, "actor": req.actor}
        upstream = await upstream_write("POST", f"/api/orders/{tid}/internal-note", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "internal_note", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/tag", response_model=ActionResponse)
async def add_tag(tid: str, req: TagRequest) -> ActionResponse:
    async def execute() -> dict[str, Any]:
        local = {"saved": True, "type": "tag", "tid": tid, "tag": req.tag, "actor": req.actor}
        upstream = await upstream_write("POST", f"/api/orders/{tid}/tag", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "tag", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/hold", response_model=ActionResponse)
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


@app.post("/api/orders/{tid}/create-shipping-draft", response_model=ActionResponse)
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
        local = {
            "saved": True,
            "type": "shipping_draft",
            "tid": tid,
            "warehouse_id": req.warehouse_id,
            "logistics_company": req.logistics_company,
            "package_note": req.package_note,
            "actor": req.actor,
            "receiver": order.buyer.model_dump(mode="json"),
            "items": [item.model_dump(mode="json") for item in order.orders],
        }
        upstream = await upstream_write("POST", f"/api/orders/{tid}/create-shipping-draft", req.model_dump(mode="json"))
        return {"local": local, "upstream": upstream}

    return await idempotent_action(tid, "create_shipping_draft", req.idempotency_key, req.actor, execute)


@app.post("/api/orders/{tid}/ship")
async def ship_order(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("ship"))


@app.post("/api/orders/{tid}/refund")
async def refund_order(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("refund"))


@app.post("/api/orders/{tid}/close")
async def close_order(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("close"))


@app.post("/api/orders/{tid}/modify-address")
async def modify_address(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("modify-address"))


@app.post("/api/orders/{tid}/modify-price")
async def modify_price(tid: str) -> dict[str, Any]:
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=deny_high_risk("modify-price"))
