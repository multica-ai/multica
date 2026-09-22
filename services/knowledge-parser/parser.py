#!/usr/bin/env python3
"""Small, private parser service for the independent knowledge domain.

The API service sends only a source file and non-secret metadata.  This process
has no database, object-store, model-provider, or user-session credentials.
The implementation intentionally keeps parsing deterministic and conservative:
it never executes office macros, formulas, scripts, or external links. PDF and
DOCX use a pinned Docling build when the service image provides it; the small
stdlib parsers remain a development fallback so the wire contract can be
tested without downloading model artifacts.
"""

from __future__ import annotations

import base64
import csv
import hashlib
import html.parser
import io
import json
import os
import posixpath
import re
import resource
import unicodedata
import zipfile
from email import policy
from email.parser import BytesParser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import unquote
from xml.etree import ElementTree as ET

try:
    import jieba  # type: ignore
except ImportError:  # pragma: no cover - development fallback without image deps
    jieba = None

try:
    import fitz  # type: ignore
except ImportError:  # pragma: no cover - source checkout without render deps
    fitz = None


SCHEMA_VERSION = "knowledge-document-v1"
PARSER_VERSION = "multica-parser-python-v2-docling"
TOKENIZER_VERSION = "jieba-search-v1"
MAX_INPUT_BYTES = int(os.environ.get("KNOWLEDGE_PARSER_MAX_INPUT_BYTES", str(128 << 20)))
MAX_OUTPUT_BYTES = int(os.environ.get("KNOWLEDGE_PARSER_MAX_OUTPUT_BYTES", str(128 << 20)))
MAX_SOURCE_BYTES = 100 << 20
MAX_ARCHIVE_ENTRIES = 20_000
MAX_ARCHIVE_BYTES = 1 << 30
MAX_ARCHIVE_MEMBER_BYTES = 128 << 20
MAX_PDF_PAGES = 1_000
MAX_RENDER_PAGES = 4
MAX_RENDER_PAGE_BYTES = 8 << 20
MAX_RENDER_BYTES = 16 << 20
PDF_PAGE_PATTERN = re.compile(rb"/Type\s*/Page\b")
TOKEN = os.environ.get("KNOWLEDGE_PARSER_TOKEN", "").strip()
DOCLING_ENABLED = os.environ.get("KNOWLEDGE_PARSER_DOCLING", "true").strip().lower() not in {"0", "false", "no"}
DOCLING_ARTIFACTS_PATH = os.environ.get("DOCLING_ARTIFACTS_PATH", "").strip()
DOCLING_REQUIRE_LOCAL_ARTIFACTS = os.environ.get("DOCLING_REQUIRE_LOCAL_ARTIFACTS", "true").strip().lower() in {"1", "true", "yes"}
DOCLING_ARTIFACTS_MANIFEST = os.environ.get("DOCLING_ARTIFACTS_MANIFEST", "").strip()


def verify_docling_artifacts() -> None:
    if not DOCLING_ENABLED or not DOCLING_REQUIRE_LOCAL_ARTIFACTS:
        return
    root = Path(DOCLING_ARTIFACTS_PATH)
    manifest_path = Path(DOCLING_ARTIFACTS_MANIFEST) if DOCLING_ARTIFACTS_MANIFEST else root / "SHA256SUMS"
    if not root.is_dir() or not manifest_path.is_file():
        raise RuntimeError("preloaded Docling artifacts and their checksum manifest are required")
    try:
        entries = manifest_path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise RuntimeError("Docling artifact checksum manifest cannot be read") from exc
    checked = 0
    for entry in entries:
        parts = entry.strip().split(maxsplit=1)
        if len(parts) != 2:
            continue
        expected, relative_name = parts
        relative_name = relative_name.lstrip("*")
        target = (root / relative_name).resolve()
        if root.resolve() not in target.parents or not target.is_file():
            raise RuntimeError("Docling artifact checksum manifest references an invalid file")
        digest = hashlib.sha256(target.read_bytes()).hexdigest()
        if digest != expected:
            raise RuntimeError(f"Docling artifact checksum mismatch: {relative_name}")
        checked += 1
    if checked == 0:
        raise RuntimeError("Docling artifact checksum manifest is empty")


def local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def text_value(value: str) -> str:
    return " ".join(value.replace("\r\n", "\n").replace("\r", "\n").split())


def format_from_extension(extension: str) -> str:
    return {
        ".md": "markdown",
        ".markdown": "markdown",
        ".mdown": "markdown",
        ".txt": "text",
        ".html": "html",
        ".htm": "html",
        ".csv": "csv",
        ".docx": "docx",
        ".xlsx": "xlsx",
        ".pdf": "pdf",
    }.get(extension, "")


def format_from_mime(mime_type: str) -> str:
    mime_type = mime_type.split(";", 1)[0].strip().lower()
    if mime_type == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
        return "docx"
    if mime_type == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
        return "xlsx"
    if mime_type == "application/pdf":
        return "pdf"
    if mime_type == "text/html":
        return "html"
    if mime_type == "text/csv":
        return "csv"
    if mime_type.startswith("text/"):
        return "text"
    return ""


def validate_source_envelope(data: bytes, filename: str, mime_type: str) -> str:
    if not data:
        raise ValueError("empty source")
    if len(data) > MAX_SOURCE_BYTES:
        raise ValueError("source exceeds the 100 MiB limit")
    extension = PurePosixPath(filename.lower()).suffix
    extension_format = format_from_extension(extension)
    mime_format = format_from_mime(mime_type)
    if extension_format and mime_format and extension_format != mime_format:
        if not (extension_format == "markdown" and mime_format == "text"):
            raise ValueError("filename extension and content type identify different formats")
    source_format = extension_format or mime_format
    if source_format == "pdf" and not data.lstrip().startswith(b"%PDF-"):
        raise ValueError("invalid PDF header")
    if source_format in {"docx", "xlsx"} and not data.startswith(b"PK"):
        raise ValueError("invalid office archive header")
    if source_format == "pdf":
        pages = len(PDF_PAGE_PATTERN.findall(data))
        if pages > MAX_PDF_PAGES:
            raise ValueError(f"PDF exceeds the {MAX_PDF_PAGES} page limit")
    return source_format


def block(block_id: str, kind: str, text: str, locator: dict[str, Any], heading_path: list[str] | None = None) -> dict[str, Any]:
    result: dict[str, Any] = {
        "block_id": block_id,
        "kind": kind,
        "text": text,
        "locator": locator,
    }
    if heading_path:
        result["heading_path"] = heading_path[:]
    return result


def document(title: str, blocks: list[dict[str, Any]], warnings: list[str] | None = None, stats: dict[str, Any] | None = None) -> dict[str, Any]:
    result = {
        "schema_version": SCHEMA_VERSION,
        "parser_version": PARSER_VERSION,
        "title": title,
        "blocks": blocks,
        "warnings": warnings or [],
        "stats": stats or {},
    }
    result["stats"]["blocks"] = len(blocks)
    return result


def decode_text(data: bytes) -> str:
    for encoding in ("utf-8-sig", "utf-8"):
        try:
            return data.decode(encoding)
        except UnicodeDecodeError:
            pass
    # CSVs from Chinese office tools are often GB18030. Use an actual
    # confidence-bearing detector when it is available; never silently use a
    # guessed legacy encoding because replacement characters would poison
    # search and evidence quotes.
    try:
        from charset_normalizer import from_bytes  # type: ignore

        match = from_bytes(data).best()
        if match is not None and match.encoding and match.percent_coherence >= 50:
            return str(match)
    except ImportError:
        pass
    raise ValueError("source encoding could not be determined; please re-upload as UTF-8")


def parse_plain(data: bytes, filename: str) -> dict[str, Any]:
    lines = decode_text(data).replace("\r\n", "\n").replace("\r", "\n").split("\n")
    blocks = [
        block(f"line-{number}", "paragraph", line, {"kind": "text", "line": number})
        for number, line in enumerate(lines, 1)
        if line.strip()
    ]
    return document(PurePosixPath(filename).stem, blocks)


def parse_markdown(data: bytes, filename: str) -> dict[str, Any]:
    lines = decode_text(data).replace("\r\n", "\n").replace("\r", "\n").split("\n")
    blocks: list[dict[str, Any]] = []
    heading_path: list[str] = []
    paragraph: list[str] = []
    paragraph_start = 1

    def flush(end_line: int) -> None:
        nonlocal paragraph
        value = "\n".join(paragraph).strip()
        if value:
            blocks.append(block(
                f"line-{paragraph_start}-{end_line}",
                "paragraph",
                value,
                {"kind": "text", "line_start": paragraph_start, "line_end": end_line},
                heading_path,
            ))
        paragraph = []

    for number, line in enumerate(lines, 1):
        stripped = line.strip()
        match = re.match(r"^(#{1,6})\s+(.+?)\s*$", stripped)
        if match:
            flush(number - 1)
            level = len(match.group(1))
            heading_path = heading_path[: level - 1]
            heading_path.append(match.group(2))
            blocks.append(block(f"heading-{number}", "heading", match.group(2), {"kind": "text", "line": number}, heading_path))
        elif not stripped:
            flush(number - 1)
        else:
            if not paragraph:
                paragraph_start = number
            paragraph.append(line)
    flush(len(lines))
    return document(PurePosixPath(filename).stem, blocks)


class HTMLBlocks(html.parser.HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.blocks: list[dict[str, Any]] = []
        self.heading_path: list[str] = []
        self.title = ""
        self.current: list[str] = []
        self.current_tag = ""
        self.skip = 0
        self.block_number = 0

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        tag = tag.lower()
        if tag in ("script", "style", "noscript"):
            self.skip += 1
            return
        if self.skip:
            return
        if tag in ("title", "h1", "h2", "h3", "h4", "p", "li", "blockquote", "pre", "td", "th"):
            self.current = []
            self.current_tag = tag

    def handle_data(self, data: str) -> None:
        if not self.skip and self.current_tag:
            self.current.append(data)

    def handle_endtag(self, tag: str) -> None:
        tag = tag.lower()
        if tag in ("script", "style", "noscript"):
            self.skip = max(0, self.skip - 1)
            return
        if self.skip or tag != self.current_tag:
            return
        value = text_value(" ".join(self.current))
        current_tag = self.current_tag
        self.current = []
        self.current_tag = ""
        if not value:
            return
        if current_tag == "title":
            self.title = value
            return
        self.block_number += 1
        if current_tag.startswith("h"):
            level = int(current_tag[1:])
            self.heading_path = self.heading_path[: level - 1]
            self.heading_path.append(value)
            self.blocks.append(block(f"heading-{self.block_number}", "heading", value, {"kind": "web", "tag": current_tag}, self.heading_path))
        else:
            kind = "table_cell" if current_tag in ("li", "td", "th") else "paragraph"
            self.blocks.append(block(f"block-{self.block_number}", kind, value, {"kind": "web", "tag": current_tag}, self.heading_path))


def parse_html(data: bytes, filename: str) -> dict[str, Any]:
    parser = HTMLBlocks()
    parser.feed(decode_text(data))
    parser.close()
    return document(parser.title or PurePosixPath(filename).stem, parser.blocks)


def unsafe_office_package(data: bytes) -> bool:
    try:
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            names = {name.lower() for name in archive.namelist()}
    except zipfile.BadZipFile:
        return False
    return any("vbaproject" in name or "externallinks" in name or name.endswith("/external links") for name in names)


def docling_label(item: Any) -> str:
    value = getattr(item, "label", "paragraph")
    value = getattr(value, "value", value)
    return str(value).lower().replace("-", "_")


def docling_item_text(item: Any, doc: Any, label: str) -> str:
    value = getattr(item, "text", "") or ""
    if label in {"table", "document_index"} and not str(value).strip():
        exporter = getattr(item, "export_to_markdown", None)
        if callable(exporter):
            try:
                value = exporter()
            except TypeError:
                try:
                    value = exporter(doc)
                except Exception:
                    value = ""
            except Exception:
                value = ""
    return str(value).strip()


def docling_locator(item: Any, kind: str, block_id: str, ordinal: int) -> tuple[dict[str, Any], int]:
    locator: dict[str, Any] = {"kind": kind, "block_id": block_id, "item": ordinal}
    max_page = 0
    provenance = getattr(item, "prov", None) or []
    if provenance:
        first = provenance[0]
        page = getattr(first, "page_no", None)
        if isinstance(page, int) and page > 0:
            locator["page"] = page
            max_page = page
        bbox = getattr(first, "bbox", None)
        if bbox is not None:
            coordinates = {}
            for name in ("l", "t", "r", "b"):
                value = getattr(bbox, name, None)
                if isinstance(value, (int, float)):
                    coordinates[name] = value
            if len(coordinates) == 4:
                locator["bbox"] = coordinates
    return locator, max_page


def parse_docling(data: bytes, filename: str, extension: str) -> dict[str, Any] | None:
    if not DOCLING_ENABLED:
        return None
    try:
        from docling.datamodel.base_models import InputFormat  # type: ignore
        from docling.datamodel.pipeline_options import PdfPipelineOptions  # type: ignore
        from docling.document_converter import DocumentConverter, DocumentStream, PdfFormatOption  # type: ignore
    except ImportError:
        # The stdlib parser is intentionally retained for source checkouts and
        # unit tests. The production image installs the locked Docling layer.
        return None
    if DOCLING_REQUIRE_LOCAL_ARTIFACTS and (not DOCLING_ARTIFACTS_PATH or not Path(DOCLING_ARTIFACTS_PATH).is_dir()):
        raise ValueError("local Docling model artifacts are required but unavailable")

    converter: Any
    if extension == ".pdf":
        options: dict[str, Any] = {"do_ocr": False, "do_table_structure": True}
        if DOCLING_ARTIFACTS_PATH:
            options["artifacts_path"] = Path(DOCLING_ARTIFACTS_PATH)
        try:
            pipeline_options = PdfPipelineOptions(**options)
        except TypeError:
            # Keep compatibility with a pinned Docling minor that exposes the
            # artifacts path as a mutable pipeline option.
            options.pop("artifacts_path", None)
            pipeline_options = PdfPipelineOptions(**options)
            if DOCLING_ARTIFACTS_PATH:
                pipeline_options.artifacts_path = Path(DOCLING_ARTIFACTS_PATH)
        converter = DocumentConverter(format_options={InputFormat.PDF: PdfFormatOption(pipeline_options=pipeline_options)})
    else:
        converter = DocumentConverter()

    source = DocumentStream(name=PurePosixPath(filename).name, stream=io.BytesIO(data))
    result = converter.convert(source)
    parsed_document = result.document
    blocks: list[dict[str, Any]] = []
    heading_path: list[str] = []
    pages = 0
    for ordinal, pair in enumerate(parsed_document.iterate_items(), 1):
        item, level = pair if isinstance(pair, tuple) else (pair, 0)
        label = docling_label(item)
        text = docling_item_text(item, parsed_document, label)
        if not text:
            continue
        if label in {"section_header", "title", "heading"}:
            kind = "heading"
            try:
                heading_level = max(1, int(level))
            except (TypeError, ValueError):
                heading_level = 1
            heading_path = heading_path[: heading_level - 1]
            heading_path.append(text)
        elif label in {"table", "document_index"}:
            kind = "table"
        elif label in {"list_item", "list"}:
            kind = "list_item"
        else:
            kind = "paragraph"
        block_id = f"docling-{ordinal}"
        locator, page = docling_locator(item, "pdf" if extension == ".pdf" else "document", block_id, ordinal)
        pages = max(pages, page)
        blocks.append(block(block_id, kind, text, locator, heading_path if kind != "heading" else heading_path))
    warnings: list[str] = []
    if extension == ".pdf" and not blocks:
        warnings.append("no text blocks found; scanned PDF needs OCR")
    return document(PurePosixPath(filename).stem, blocks, warnings, {"pages": pages, "docling": True})


def column_name(number: int) -> str:
    output = ""
    while number:
        number, remainder = divmod(number - 1, 26)
        output = chr(65 + remainder) + output
    return output or "A"


def parse_csv(data: bytes, filename: str) -> dict[str, Any]:
    reader = csv.reader(io.StringIO(decode_text(data)))
    blocks: list[dict[str, Any]] = []
    for row_number, row in enumerate(reader, 1):
        if not any(value.strip() for value in row):
            continue
        parts = [f"{column_name(index)}: {value.strip()}" for index, value in enumerate(row, 1)]
        blocks.append(block(
            f"row-{row_number}",
            "table_row",
            " | ".join(parts),
            {"kind": "table", "sheet": "CSV", "row": row_number, "cell_range": f"A{row_number}:{column_name(len(row))}{row_number}"},
        ))
    return document(PurePosixPath(filename).stem, blocks)


def zip_bytes(archive: zipfile.ZipFile, name: str) -> bytes:
    try:
        info = archive.getinfo(name)
    except KeyError as exc:
        raise ValueError(f"zip member {name!r} is missing") from exc
    if info.file_size > MAX_ARCHIVE_MEMBER_BYTES:
        raise ValueError(f"zip member {name!r} exceeds the 128 MiB limit")
    value = archive.read(info)
    if len(value) > MAX_ARCHIVE_MEMBER_BYTES:
        raise ValueError(f"zip member {name!r} exceeds the 128 MiB limit")
    return value


def validate_archive(archive: zipfile.ZipFile) -> None:
    infos = archive.infolist()
    if len(infos) > MAX_ARCHIVE_ENTRIES:
        raise ValueError(f"zip archive contains more than {MAX_ARCHIVE_ENTRIES} entries")
    total = 0
    names: set[str] = set()
    for info in infos:
        name = info.filename.replace("\\", "/")
        clean = posixpath.normpath(name)
        if (
            not name
            or "\x00" in name
            or posixpath.isabs(name)
            or re.match(r"^[A-Za-z]:", name)
            or clean == ".."
            or clean.startswith("../")
        ):
            raise ValueError("zip archive contains an unsafe member path")
        if name in names:
            raise ValueError("zip archive contains duplicate member names")
        names.add(name)
        if info.file_size > MAX_ARCHIVE_MEMBER_BYTES:
            raise ValueError(f"zip member {info.filename!r} exceeds the 128 MiB limit")
        total += info.file_size
        if total > MAX_ARCHIVE_BYTES:
            raise ValueError("zip archive exceeds the 1 GiB expanded-size limit")


def apply_resource_limits() -> None:
    # The backend also bounds each parser request to 900 seconds. This process
    # limit prevents a malformed document from growing the parser beyond the
    # 2 GiB deployment budget when several requests overlap.
    try:
        _soft, hard = resource.getrlimit(resource.RLIMIT_AS)
        cap = 2 << 30
        if hard != resource.RLIM_INFINITY:
            cap = min(cap, hard)
        resource.setrlimit(resource.RLIMIT_AS, (cap, hard))
    except (AttributeError, OSError, ValueError):
        # Docker's memory limit remains the deployment-level guard on systems
        # without RLIMIT_AS or where the runtime disallows changing it.
        return


def parse_docx(data: bytes, filename: str) -> dict[str, Any]:
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        validate_archive(archive)
        names = {name.lower() for name in archive.namelist()}
        if any("vbaproject" in name or name.endswith("/external links") for name in names):
            raise ValueError("macro-enabled or externally linked document is not supported")
        root = ET.fromstring(zip_bytes(archive, "word/document.xml"))
    blocks: list[dict[str, Any]] = []
    paragraph_number = 0
    for paragraph in root.iter():
        if local_name(paragraph.tag) != "p":
            continue
        value = "".join(node.text or "" for node in paragraph.iter() if local_name(node.tag) == "t").strip()
        if not value:
            continue
        paragraph_number += 1
        blocks.append(block(f"paragraph-{paragraph_number}", "paragraph", value, {"kind": "document", "paragraph": paragraph_number}))
    return document(PurePosixPath(filename).stem, blocks)


def shared_strings(root: ET.Element) -> list[str]:
    values: list[str] = []
    for item in root.iter():
        if local_name(item.tag) != "si":
            continue
        values.append("".join(node.text or "" for node in item.iter() if local_name(node.tag) == "t"))
    return values


def parse_xlsx(data: bytes, filename: str) -> dict[str, Any]:
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        validate_archive(archive)
        names = archive.namelist()
        lowered = {name.lower() for name in names}
        if any("vbaproject" in name or "externallinks" in name for name in lowered):
            raise ValueError("macro-enabled or externally linked workbook is not supported")
        shared = shared_strings(ET.fromstring(archive.read("xl/sharedStrings.xml"))) if "xl/sharedStrings.xml" in names else []
        workbook = ET.fromstring(zip_bytes(archive, "xl/workbook.xml"))
        relationships: dict[str, str] = {}
        rels_name = "xl/_rels/workbook.xml.rels"
        if rels_name in names:
            rels = ET.fromstring(archive.read(rels_name))
            for relation in rels:
                if local_name(relation.tag) == "Relationship":
                    relationships[relation.attrib.get("Id", "")] = relation.attrib.get("Target", "")
        sheets = []
        for sheet in workbook.iter():
            if local_name(sheet.tag) != "sheet":
                continue
            rel_id = next((value for key, value in sheet.attrib.items() if local_name(key) == "id"), "")
            target = relationships.get(rel_id, "")
            target = posixpath.normpath(str(PurePosixPath("xl") / target.lstrip("/"))) if not target.startswith("xl/") else target
            sheets.append((sheet.attrib.get("name", "Sheet"), sheet.attrib.get("state", "visible"), target))
        blocks: list[dict[str, Any]] = []
        warnings: list[str] = []
        sheet_count = 0
        for sheet_index, (sheet_name, state, target) in enumerate(sheets, 1):
            if state != "visible":
                warnings.append(f"hidden_sheet_not_parsed:{sheet_name}")
                continue
            if target not in names:
                warnings.append(f"sheet_missing:{sheet_name}")
                continue
            sheet_count += 1
            root = ET.fromstring(archive.read(target))
            merges = [merge.attrib.get("ref", "") for merge in root.iter() if local_name(merge.tag) == "mergeCell"]
            for row in root.iter():
                if local_name(row.tag) != "row":
                    continue
                row_number = int(row.attrib.get("r", "0") or 0)
                cells: list[str] = []
                refs: list[str] = []
                for cell in row:
                    if local_name(cell.tag) != "c":
                        continue
                    ref = cell.attrib.get("r", "")
                    cell_type = cell.attrib.get("t", "")
                    formula = next((node.text or "" for node in cell if local_name(node.tag) == "f"), "")
                    value = next((node.text or "" for node in cell if local_name(node.tag) == "v"), "")
                    if cell_type == "s" and value.isdigit() and int(value) < len(shared):
                        value = shared[int(value)]
                    elif cell_type == "inlineStr":
                        value = "".join(node.text or "" for node in cell.iter() if local_name(node.tag) == "t")
                    elif cell_type == "b":
                        value = "是" if value == "1" else "否"
                    if formula:
                        value = f"[公式:{formula}] = {value}" if value else f"[公式:{formula}] 公式未计算"
                    if not value and not formula:
                        continue
                    cells.append(f"{ref}: {value}")
                    refs.append(ref)
                if not cells:
                    continue
                locator: dict[str, Any] = {"kind": "table", "sheet": sheet_name, "row": row_number}
                locator["cell_range"] = f"{refs[0]}:{refs[-1]}"
                containing_merges = [merge for merge in merges if any(ref in merge for ref in refs)]
                if containing_merges:
                    locator["merged_ranges"] = containing_merges
                blocks.append(block(f"sheet-{sheet_index}-row-{row_number}", "table_row", " | ".join(cells), locator))
        return document(PurePosixPath(filename).stem, blocks, warnings, {"sheets": sheet_count})


def decode_pdf_literal(value: bytes) -> str:
    if value.startswith(b"(") and value.endswith(b")"):
        value = value[1:-1]
    value = re.sub(rb"\\([\\()\\])", rb"\1", value)
    value = value.replace(rb"\\n", b"\n").replace(rb"\\r", b"\r").replace(rb"\\t", b"\t")
    return value.decode("utf-8", errors="replace").strip()


def parse_pdf(data: bytes, filename: str) -> dict[str, Any]:
    if not data.lstrip().startswith(b"%PDF-"):
        raise ValueError("invalid PDF header")
    page_count = len(PDF_PAGE_PATTERN.findall(data))
    if page_count > MAX_PDF_PAGES:
        raise ValueError(f"PDF exceeds the {MAX_PDF_PAGES} page limit")
    literals = [decode_pdf_literal(match) for match in re.findall(rb"\((?:\\.|[^\\)])*\)", data)]
    paragraphs = [value for value in literals if value]
    blocks = [block(f"pdf-1-{index}", "paragraph", value, {"kind": "pdf", "page": 1, "text_index": index}) for index, value in enumerate(paragraphs, 1)]
    warnings = [] if blocks else ["no text blocks found; scanned PDF needs OCR"]
    return document(PurePosixPath(filename).stem, blocks, warnings, {"pages": page_count or (1 if blocks else 0)})


def parse_source(data: bytes, filename: str, mime_type: str) -> dict[str, Any]:
    source_format = validate_source_envelope(data, filename, mime_type)
    if source_format == "markdown":
        return parse_markdown(data, filename)
    if source_format == "text":
        return parse_plain(data, filename)
    if source_format == "html":
        return parse_html(data, filename)
    if source_format == "csv":
        return parse_csv(data, filename)
    if source_format == "docx":
        if unsafe_office_package(data):
            raise ValueError("macro-enabled or externally linked document is not supported")
        if parsed := parse_docling(data, filename, ".docx"):
            return parsed
        return parse_docx(data, filename)
    if source_format == "xlsx":
        return parse_xlsx(data, filename)
    if source_format == "pdf":
        if parsed := parse_docling(data, filename, ".pdf"):
            return parsed
        return parse_pdf(data, filename)
    raise ValueError("unsupported format")


def render_pdf_pages(data: bytes, filename: str, mime_type: str, pages: Any) -> list[dict[str, Any]]:
    """Render a small, explicitly requested PDF page set for vision models.

    Rendering is deliberately separate from parsing. The parser never has a
    model-provider credential or object-store access; it only returns bounded
    page images to the Go worker, which sends them to the user-selected model.
    """
    if validate_source_envelope(data, filename, mime_type) != "pdf":
        raise ValueError("vision page rendering currently supports PDF sources only")
    if fitz is None:
        raise ValueError("vision page rendering is unavailable in this parser image")
    if not isinstance(pages, list) or not pages:
        raise ValueError("vision rendering requires a non-empty pages list")
    normalized_pages: list[int] = []
    seen: set[int] = set()
    for value in pages:
        if isinstance(value, bool) or not isinstance(value, int) or value < 1 or value > MAX_PDF_PAGES:
            raise ValueError("vision rendering page numbers must be positive integers")
        if value in seen:
            continue
        seen.add(value)
        normalized_pages.append(value)
    if len(normalized_pages) > MAX_RENDER_PAGES:
        raise ValueError(f"vision rendering supports at most {MAX_RENDER_PAGES} pages per request")

    try:
        pdf = fitz.open(stream=data, filetype="pdf")
    except Exception as exc:  # pragma: no cover - exercised by the locked image
        raise ValueError("PDF could not be opened for vision rendering") from exc
    try:
        if pdf.page_count > MAX_PDF_PAGES:
            raise ValueError(f"PDF exceeds the {MAX_PDF_PAGES} page limit")
        result: list[dict[str, Any]] = []
        total_bytes = 0
        for page_number in normalized_pages:
            if page_number > pdf.page_count:
                raise ValueError(f"PDF page {page_number} does not exist")
            page = pdf.load_page(page_number - 1)
            pixmap = page.get_pixmap(matrix=fitz.Matrix(1.5, 1.5), alpha=False)
            image = pixmap.tobytes("png")
            if len(image) == 0 or len(image) > MAX_RENDER_PAGE_BYTES:
                raise ValueError(f"rendered PDF page {page_number} exceeds the image size limit")
            total_bytes += len(image)
            if total_bytes > MAX_RENDER_BYTES:
                raise ValueError("rendered PDF pages exceed the total image size limit")
            result.append({
                "page": page_number,
                "mime_type": "image/png",
                "data_base64": base64.b64encode(image).decode("ascii"),
                "width": pixmap.width,
                "height": pixmap.height,
            })
        return result
    finally:
        pdf.close()


def multipart_payload(content_type: str, body: bytes) -> tuple[bytes, str, dict[str, Any]]:
    envelope = b"Content-Type: " + content_type.encode() + b"\r\nMIME-Version: 1.0\r\n\r\n" + body
    message = BytesParser(policy=policy.default).parsebytes(envelope)
    source = b""
    filename = "source"
    metadata: dict[str, Any] = {}
    for part in message.walk():
        if part.is_multipart():
            continue
        name = part.get_param("name", header="content-disposition")
        value = part.get_payload(decode=True) or b""
        if name == "file":
            source = value
            filename = part.get_filename() or filename
        elif name == "metadata":
            try:
                parsed_metadata = json.loads(value.decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError) as exc:
                raise ValueError("metadata must be valid JSON") from exc
            if not isinstance(parsed_metadata, dict):
                raise ValueError("metadata must be a JSON object")
            metadata = parsed_metadata
    filename = str(metadata.get("filename") or filename)
    mime_type = str(metadata.get("mime_type") or "application/octet-stream")
    return source, filename, {"mime_type": mime_type, **metadata}


def request_payload(handler: BaseHTTPRequestHandler) -> tuple[bytes, str, dict[str, Any]]:
    try:
        length = int(handler.headers.get("Content-Length", "0") or 0)
    except ValueError as exc:
        raise ValueError("request body length is invalid") from exc
    if length <= 0 or length > MAX_INPUT_BYTES:
        raise ValueError("request body exceeds parser limit")
    body = handler.rfile.read(length)
    content_type = handler.headers.get("Content-Type", "")
    if content_type.lower().startswith("multipart/"):
        return multipart_payload(content_type, body)
    try:
        payload = json.loads(body.decode("utf-8"))
        if not isinstance(payload, dict):
            raise ValueError("parse request must be a JSON object")
        source_encoded = payload.get("source_base64", "")
        if not isinstance(source_encoded, str):
            raise ValueError("source_base64 must be a string")
        source = base64.b64decode(source_encoded, validate=True)
        if len(source) > MAX_SOURCE_BYTES:
            raise ValueError("source exceeds the 100 MiB limit")
        if len(body) != length:
            raise ValueError("request body is incomplete")
        metadata = dict(payload)
        metadata["mime_type"] = str(payload.get("mime_type", "application/octet-stream"))
        metadata["filename"] = str(payload.get("filename", "source"))
        return source, metadata["filename"], metadata
    except (UnicodeDecodeError, json.JSONDecodeError, ValueError) as exc:
        raise ValueError("parse requires multipart file and metadata") from exc


def search_tokens(text: str) -> list[str]:
    normalized = unicodedata.normalize("NFKC", text)
    if jieba is not None:
        pieces = jieba.cut_for_search(normalized, HMM=False)
    else:
        # Keep protocol tests runnable from a source checkout without pip
        # dependencies. The production image installs the pinned Jieba
        # package; this fallback is conservative for Chinese text.
        pieces = re.findall(r"[\u4e00-\u9fff]|[A-Za-z0-9_]+|[^\s]", normalized)
    tokens: list[str] = []
    for piece in pieces:
        piece = piece.strip()
        if not piece or not any(character.isalnum() for character in piece):
            continue
        if re.fullmatch(r"[A-Za-z0-9_]+", piece):
            piece = piece.lower()
        tokens.append(piece)
    return tokens


def tokenize_text(text: str) -> list[str]:
    if len(text) > 4_000:
        raise ValueError("tokenize text exceeds the 4000 character limit")
    return search_tokens(text)


def json_response(handler: BaseHTTPRequestHandler, status: int, payload: dict[str, Any]) -> None:
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    if len(body) > MAX_OUTPUT_BYTES:
        status = 422
        body = b'{"error":"parse_output_too_large","code":"parse_output_too_large"}'
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


class ParserHandler(BaseHTTPRequestHandler):
    server_version = "MulticaKnowledgeParser/1"

    def log_message(self, _format: str, *_args: Any) -> None:
        return

    def authorized(self) -> bool:
        return not TOKEN or self.headers.get("Authorization", "") == f"Bearer {TOKEN}"

    def do_GET(self) -> None:  # noqa: N802
        if self.path != "/health":
            json_response(self, 404, {"error": "not_found"})
            return
        json_response(self, 200, {"status": "ok", "schema_version": SCHEMA_VERSION, "parser_version": PARSER_VERSION, "tokenizer_version": TOKENIZER_VERSION})

    def do_POST(self) -> None:  # noqa: N802
        if not self.authorized():
            json_response(self, 401, {"error": "unauthorized"})
            return
        try:
            if self.path == "/v1/parse":
                source, filename, metadata = request_payload(self)
                parsed = parse_source(source, filename, str(metadata.get("mime_type", "application/octet-stream")))
                json_response(self, 200, parsed)
                return
            if self.path == "/v1/render":
                source, filename, metadata = request_payload(self)
                images = render_pdf_pages(source, filename, str(metadata.get("mime_type", "application/octet-stream")), metadata.get("pages"))
                json_response(self, 200, {"images": images})
                return
            if self.path == "/v1/tokenize":
                length = int(self.headers.get("Content-Length", "0") or 0)
                if length <= 0 or length > MAX_INPUT_BYTES:
                    raise ValueError("tokenize request exceeds parser limit")
                body = self.rfile.read(length)
                if len(body) != length:
                    raise ValueError("tokenize request body is incomplete")
                raw = json.loads(body.decode("utf-8"))
                if not isinstance(raw, dict) or not isinstance(raw.get("text"), str):
                    raise ValueError("tokenize requires a text string")
                tokens = tokenize_text(raw["text"])
                json_response(self, 200, {"tokens": tokens, "tokenizer_version": TOKENIZER_VERSION})
                return
            json_response(self, 404, {"error": "not_found"})
        except ValueError as exc:
            if self.path == "/v1/tokenize":
                code = "tokenize_failed"
            elif self.path == "/v1/render":
                code = "vision_render_unavailable" if "unavailable" in str(exc).lower() or "supports PDF" in str(exc) else "vision_render_failed"
            else:
                code = "unsupported_format" if str(exc) == "unsupported format" else "parse_failed"
            json_response(self, 422, {"error": str(exc), "code": code})
        except (zipfile.BadZipFile, ET.ParseError) as exc:
            json_response(self, 422, {"error": "source archive is invalid", "code": "parse_failed", "detail": str(exc)})
        except Exception:
            json_response(self, 500, {"error": "parser failed", "code": "parser_failed"})


def main() -> None:
    apply_resource_limits()
    verify_docling_artifacts()
    port = int(os.environ.get("PORT", "8091"))
    server = ThreadingHTTPServer((os.environ.get("HOST", "0.0.0.0"), port), ParserHandler)
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
