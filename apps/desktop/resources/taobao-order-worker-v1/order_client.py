from __future__ import annotations

import asyncio
import contextlib
import json
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import httpx


class OrderNormalizationError(ValueError):
    pass


class UpstreamOrderAPIError(Exception):
    def __init__(self, status_code: int, detail: Any) -> None:
        super().__init__(str(detail))
        self.status_code = status_code
        self.detail = detail


@dataclass(frozen=True)
class OrderApiClientConfig:
    base_url: str
    adapter: str = "http"
    token: str = ""
    get_order_path: str = "/api/orders/{tid}"
    timeout_seconds: float = 10.0
    auth_header: str = "Authorization"
    auth_scheme: str = "Bearer"
    taobao_cli_repo_path: str = ""
    taobao_cli_cookie_file: str = ""
    taobao_cli_python: str = sys.executable


def _first_present(mapping: dict[str, Any], paths: list[str], default: Any = None) -> Any:
    for path in paths:
        value = _get_path(mapping, path)
        if value is not None:
            return value
    return default


def _get_path(mapping: dict[str, Any], path: str) -> Any:
    current: Any = mapping
    for part in path.split("."):
        if not isinstance(current, dict) or part not in current:
            return None
        current = current[part]
    return current


def _as_str(value: Any, default: str = "") -> str:
    if value is None:
        return default
    return str(value)


def _find_first_recursive(value: Any, keys: tuple[str, ...], max_depth: int = 8) -> Any:
    if max_depth < 0:
        return None
    if isinstance(value, dict):
        for key in keys:
            if key in value and value[key] not in (None, ""):
                return value[key]
        for child in value.values():
            found = _find_first_recursive(child, keys, max_depth=max_depth - 1)
            if found not in (None, ""):
                return found
    elif isinstance(value, list):
        for child in value:
            found = _find_first_recursive(child, keys, max_depth=max_depth - 1)
            if found not in (None, ""):
                return found
    return None


def _canonical_taobao_status(status: Any) -> str:
    raw = _as_str(status).strip()
    if not raw:
        return ""
    if re.fullmatch(r"[A-Z_]+", raw):
        return raw
    if any(token in raw for token in ("待发货", "等待卖家发货", "买家已付款", "已付款")):
        return "WAIT_SELLER_SEND_GOODS"
    if any(token in raw for token in ("待付款", "等待买家付款")):
        return "WAIT_BUYER_PAY"
    if any(token in raw for token in ("已发货", "等待买家确认", "待收货")):
        return "WAIT_BUYER_CONFIRM_GOODS"
    if any(token in raw for token in ("交易成功", "交易完成", "已完成")):
        return "TRADE_FINISHED"
    if any(token in raw for token in ("交易关闭", "已关闭", "关闭")):
        return "TRADE_CLOSED"
    if any(token in raw for token in ("禁止发货", "不可发货")):
        return "PAID_FORBID_CONSIGN"
    return raw


def _match_raw_order(raw_orders: Any, tid: str) -> dict[str, Any]:
    if not isinstance(raw_orders, list):
        return {}
    for raw_order in raw_orders:
        if not isinstance(raw_order, dict):
            continue
        raw_id = _find_first_recursive(raw_order, ("id", "idStr", "orderId", "order_id", "bizOrderId"), max_depth=4)
        if _as_str(raw_id) == tid:
            return raw_order
    return raw_orders[0] if raw_orders and isinstance(raw_orders[0], dict) else {}


def normalize_taobao_cli_payload(payload: dict[str, Any], tid: str) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise OrderNormalizationError("taobao CLI payload must be an object")

    orders = payload.get("orders")
    if not isinstance(orders, list) or not orders:
        raise OrderNormalizationError("taobao CLI did not return any matching orders")

    selected = None
    for order in orders:
        if isinstance(order, dict) and _as_str(order.get("order_id")) == tid:
            selected = order
            break
    if selected is None:
        selected = orders[0] if isinstance(orders[0], dict) else None
    if selected is None:
        raise OrderNormalizationError("taobao CLI order row must be an object")

    raw_response = payload.get("raw") if isinstance(payload.get("raw"), dict) else {}
    raw_order = _match_raw_order(raw_response.get("mainOrders"), _as_str(selected.get("order_id") or tid))

    buyer_nick = selected.get("buyer_nick") or _find_first_recursive(raw_order, ("nick", "buyerNick", "buyer_nick"))
    receiver_name = _find_first_recursive(
        raw_order,
        ("receiverName", "receiver_name", "consigneeName", "name", "recipientName"),
    )
    receiver_mobile = _find_first_recursive(
        raw_order,
        ("receiverMobile", "receiver_mobile", "mobile", "phone", "consigneeMobile"),
    )
    receiver_state = _find_first_recursive(raw_order, ("receiverState", "receiver_state", "province", "state"))
    receiver_city = _find_first_recursive(raw_order, ("receiverCity", "receiver_city", "city"))
    receiver_district = _find_first_recursive(raw_order, ("receiverDistrict", "receiver_district", "district", "area"))
    receiver_address = _find_first_recursive(
        raw_order,
        ("receiverAddress", "receiver_address", "address", "detailAddress", "addressDetail"),
    )

    upstream_items: list[dict[str, Any]] = []
    for index, item in enumerate(selected.get("items") or []):
        if not isinstance(item, dict):
            continue
        sku = item.get("sku")
        sku_text = ";".join(_as_str(part) for part in sku) if isinstance(sku, list) else _as_str(sku)
        upstream_items.append(
            {
                "oid": item.get("sub_order_id") or f"{tid}-{index + 1}",
                "sku_id": item.get("sku_id"),
                "title": item.get("title"),
                "sku_properties_name": sku_text,
                "num": item.get("quantity") or 1,
                "price": item.get("price"),
                "payment": item.get("price"),
                "refund_status": "NO_REFUND",
                "inventory_status": "RESERVED",
            }
        )

    return {
        "tid": _as_str(selected.get("order_id") or tid),
        "platform": "taobao",
        "shop_id": _as_str(_find_first_recursive(raw_order, ("shopId", "sellerId", "seller_id"), max_depth=5), ""),
        "status": _canonical_taobao_status(selected.get("status")),
        "payment": selected.get("actual_fee"),
        "buyer": {
            "buyer_nick": buyer_nick,
            "buyer_open_id": _find_first_recursive(raw_order, ("buyerOpenId", "buyerId", "buyer_id"), max_depth=5),
            "receiver_oaid": _find_first_recursive(raw_order, ("oaid", "receiverOaid", "receiver_oaid"), max_depth=5),
            "receiver_name": receiver_name,
            "receiver_mobile": receiver_mobile,
            "receiver_phone": None,
            "receiver_state": receiver_state,
            "receiver_city": receiver_city,
            "receiver_district": receiver_district,
            "receiver_address": receiver_address,
            "receiver_zip": _find_first_recursive(raw_order, ("receiverZip", "zip", "postcode"), max_depth=5),
        },
        "orders": upstream_items,
        "buyer_message": _find_first_recursive(raw_order, ("buyerMessage", "buyer_message", "buyerMemo"), max_depth=6),
        "seller_memo": _find_first_recursive(raw_order, ("sellerMemo", "seller_memo", "sellerNote"), max_depth=6),
        "created": selected.get("create_time") or _find_first_recursive(raw_order, ("createTime", "created"), max_depth=5),
        "modified": _find_first_recursive(raw_order, ("modified", "modifiedTime", "updateTime"), max_depth=5),
    }


def _normalize_buyer(upstream: dict[str, Any]) -> dict[str, Any]:
    buyer = _first_present(upstream, ["buyer", "receiver", "receive_info", "recipient"], {})
    if not isinstance(buyer, dict):
        buyer = {}
    merged = {**upstream, **buyer}
    return {
        "buyer_nick": _first_present(merged, ["buyer_nick", "buyerNick", "buyer_nickname", "nick"]),
        "buyer_open_id": _first_present(merged, ["buyer_open_id", "buyerOpenId", "buyer_id", "buyerId"]),
        "receiver_oaid": _first_present(merged, ["receiver_oaid", "receiverOaid", "oaid"]),
        "receiver_name": _first_present(merged, ["receiver_name", "receiverName", "name", "recipientName"]),
        "receiver_mobile": _first_present(merged, ["receiver_mobile", "receiverMobile", "mobile", "phone"]),
        "receiver_phone": _first_present(merged, ["receiver_phone", "receiverPhone", "tel"]),
        "receiver_state": _first_present(merged, ["receiver_state", "receiverState", "province", "state"]),
        "receiver_city": _first_present(merged, ["receiver_city", "receiverCity", "city"]),
        "receiver_district": _first_present(merged, ["receiver_district", "receiverDistrict", "district", "area"]),
        "receiver_address": _first_present(merged, ["receiver_address", "receiverAddress", "address", "detailAddress"]),
        "receiver_zip": _first_present(merged, ["receiver_zip", "receiverZip", "zip", "postcode"]),
    }


def _normalize_items(upstream: dict[str, Any]) -> list[dict[str, Any]]:
    items = _first_present(upstream, ["orders", "items", "sub_orders", "subOrders", "order_items"], [])
    if not isinstance(items, list):
        raise OrderNormalizationError("order items must be a list")
    if not items:
        raise OrderNormalizationError("order items must not be empty")
    normalized: list[dict[str, Any]] = []
    for index, item in enumerate(items):
        if not isinstance(item, dict):
            raise OrderNormalizationError(f"order item at index {index} must be an object")
        normalized.append(
            {
                "oid": _as_str(_first_present(item, ["oid", "order_id", "orderId", "id"], f"item-{index + 1}")),
                "sku_id": _first_present(item, ["sku_id", "skuId", "sku"]),
                "outer_sku_id": _first_present(item, ["outer_sku_id", "outerSkuId", "outerSku", "merchant_sku"]),
                "title": _first_present(item, ["title", "item_title", "itemTitle", "name"]),
                "sku_properties_name": _first_present(
                    item, ["sku_properties_name", "skuPropertiesName", "sku_props", "spec"]
                ),
                "num": int(_first_present(item, ["num", "quantity", "qty"], 1) or 1),
                "price": _first_present(item, ["price", "item_price", "itemPrice"]),
                "payment": _first_present(item, ["payment", "paid_fee", "paidFee"]),
                "refund_status": _first_present(item, ["refund_status", "refundStatus"], "NO_REFUND"),
                "inventory_status": _first_present(item, ["inventory_status", "inventoryStatus"], "RESERVED"),
            }
        )
    return normalized


def normalize_order(upstream: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(upstream, dict):
        raise OrderNormalizationError("upstream order payload must be an object")
    tid = _first_present(upstream, ["tid", "trade_id", "tradeId", "order_id", "orderId", "id"])
    status = _first_present(upstream, ["status", "order_status", "orderStatus", "trade_status", "tradeStatus"])
    if tid is None:
        raise OrderNormalizationError("missing order id: expected tid/trade_id/order_id")
    if status is None:
        raise OrderNormalizationError("missing order status: expected status/order_status/trade_status")

    return {
        "tid": _as_str(tid),
        "platform": _as_str(_first_present(upstream, ["platform"], "taobao"), "taobao"),
        "shop_id": _as_str(_first_present(upstream, ["shop_id", "shopId", "seller_id", "sellerId", "shop.id"], "")),
        "status": _as_str(status),
        "payment": _first_present(upstream, ["payment", "paid_fee", "paidFee", "total_fee", "totalFee"]),
        "buyer": _normalize_buyer(upstream),
        "orders": _normalize_items(upstream),
        "buyer_message": _first_present(upstream, ["buyer_message", "buyerMessage", "buyer_memo"]),
        "seller_memo": _first_present(upstream, ["seller_memo", "sellerMemo", "seller_note"]),
        "created": _first_present(upstream, ["created", "created_at", "createdAt"]),
        "modified": _first_present(upstream, ["modified", "modified_at", "modifiedAt", "updated_at", "updatedAt"]),
    }


class OrderApiClient:
    def __init__(self, config: OrderApiClientConfig, transport: httpx.AsyncBaseTransport | None = None) -> None:
        self.config = config
        self._transport = transport

    def _headers(self) -> dict[str, str]:
        if not self.config.token:
            return {}
        value = self.config.token
        if self.config.auth_scheme:
            value = f"{self.config.auth_scheme} {value}"
        return {self.config.auth_header: value}

    async def _request(self, method: str, path: str, **kwargs: Any) -> httpx.Response:
        url = f"{self.config.base_url.rstrip('/')}{path}"
        try:
            async with httpx.AsyncClient(
                timeout=self.config.timeout_seconds,
                transport=self._transport,
            ) as client:
                return await client.request(method, url, headers=self._headers(), **kwargs)
        except httpx.TimeoutException:
            raise UpstreamOrderAPIError(504, "order API timeout") from None
        except httpx.RequestError as exc:
            raise UpstreamOrderAPIError(502, {"message": "order API request failed", "error": str(exc)}) from exc

    async def _run_taobao_cli_order(self, tid: str) -> dict[str, Any]:
        repo = Path(self.config.taobao_cli_repo_path).expanduser()
        if not repo.is_dir():
            raise UpstreamOrderAPIError(502, "taobao CLI repo path is not configured or does not exist")

        cookie_file = Path(self.config.taobao_cli_cookie_file).expanduser()
        if not cookie_file.is_absolute():
            cookie_file = repo / cookie_file
        if not cookie_file.is_file():
            raise UpstreamOrderAPIError(502, "taobao CLI cookie file is not configured or does not exist")

        python = self.config.taobao_cli_python or sys.executable
        cmd = [
            python,
            "-m",
            "taobao_cli",
            "orders",
            "order",
            "--cookie-file",
            str(cookie_file),
            "--order-id",
            tid,
            "--page-num",
            "1",
            "--page-size",
            "1",
            "--include-raw",
        ]
        env = os.environ.copy()
        env["PYTHONPATH"] = str(repo) + (os.pathsep + env["PYTHONPATH"] if env.get("PYTHONPATH") else "")
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                cwd=str(repo),
                env=env,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=self.config.timeout_seconds)
        except asyncio.TimeoutError:
            with contextlib.suppress(Exception):
                proc.kill()  # type: ignore[possibly-undefined]
            raise UpstreamOrderAPIError(504, "taobao CLI order query timeout") from None
        except OSError as exc:
            raise UpstreamOrderAPIError(502, {"message": "taobao CLI failed to start", "error": str(exc)}) from exc

        if proc.returncode != 0:
            detail = stderr.decode("utf-8", errors="replace").strip().splitlines()[-1:] or ["taobao CLI failed"]
            raise UpstreamOrderAPIError(502, {"message": "taobao CLI order query failed", "error": detail[0]})
        try:
            return json.loads(stdout.decode("utf-8"))
        except ValueError as exc:
            raise UpstreamOrderAPIError(502, {"message": "taobao CLI returned non-JSON output"}) from exc

    @staticmethod
    def _body(resp: httpx.Response) -> str:
        return resp.text[:2000]

    @classmethod
    def _raise_for_read(cls, resp: httpx.Response) -> None:
        if resp.status_code == 404:
            raise UpstreamOrderAPIError(404, "order not found")
        if resp.status_code in {401, 403}:
            raise UpstreamOrderAPIError(502, "order API upstream auth failed")
        if not 200 <= resp.status_code < 300:
            raise UpstreamOrderAPIError(
                502,
                {"message": "order API returned non-2xx", "status_code": resp.status_code, "body": cls._body(resp)},
            )

    @classmethod
    def _raise_for_write(cls, resp: httpx.Response) -> None:
        if not 200 <= resp.status_code < 300:
            raise UpstreamOrderAPIError(
                502,
                {
                    "message": "order API write returned non-2xx",
                    "status_code": resp.status_code,
                    "body": cls._body(resp),
                },
            )

    async def get_order(self, tid: str, plain: bool = True) -> dict[str, Any]:
        if self.config.adapter == "taobao_cli":
            try:
                return normalize_order(normalize_taobao_cli_payload(await self._run_taobao_cli_order(tid), tid))
            except OrderNormalizationError as exc:
                raise UpstreamOrderAPIError(
                    502,
                    {"message": "taobao CLI order normalization failed", "error": str(exc)},
                ) from exc

        path = self.config.get_order_path.format(tid=tid)
        resp = await self._request("GET", path, params={"plain": str(plain).lower()})
        self._raise_for_read(resp)
        try:
            return normalize_order(resp.json())
        except (ValueError, TypeError) as exc:
            raise UpstreamOrderAPIError(
                502,
                {"message": "order API normalization failed", "error": str(exc)},
            ) from exc

    async def write_action(self, method: str, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        if self.config.adapter == "taobao_cli":
            raise UpstreamOrderAPIError(501, "taobao CLI adapter is read-only")
        resp = await self._request(method, path, json=payload)
        self._raise_for_write(resp)
        return {"status_code": resp.status_code, "body": self._body(resp)}

    async def add_internal_note(self, tid: str, payload: dict[str, Any]) -> dict[str, Any]:
        return await self.write_action("POST", f"/api/orders/{tid}/internal-note", payload)

    async def add_tag(self, tid: str, payload: dict[str, Any]) -> dict[str, Any]:
        return await self.write_action("POST", f"/api/orders/{tid}/tag", payload)

    async def hold_order(self, tid: str, payload: dict[str, Any]) -> dict[str, Any]:
        return await self.write_action("POST", f"/api/orders/{tid}/hold", payload)

    async def create_shipping_draft(self, tid: str, payload: dict[str, Any]) -> dict[str, Any]:
        return await self.write_action("POST", f"/api/orders/{tid}/create-shipping-draft", payload)
