# Multica knowledge parser

This is an internal, stateless HTTP service. The Go knowledge worker sends a
`multipart/form-data` request to `POST /v1/parse` with:

- `file`: the source bytes;
- `metadata`: JSON containing `filename` and `mime_type`.

The same multipart envelope can be sent to `POST /v1/render` with a metadata
`pages` array. It returns at most four bounded PNG page images for an enabled
vision enhancement. Rendering is currently limited to PDF sources; the Go
worker treats an unavailable renderer as an explicit optional-enhancement
skip, while parser and keyword indexing remain usable.

The response is the versioned `ParsedDocument` JSON object used by the Go
worker. `GET /health` is a liveness check and `POST /v1/tokenize` provides the
stable `jieba-search-v1` search-token contract used by keyword queries. The
API service keeps normalized source text in the keyword index and sends query
text through the same tokenizer contract.

The container has no database, object-store, model-provider, or user-session
credentials. It does not execute macros, formulas, or external links. PDF and
DOCX conversion uses the pinned Docling dependency in `requirements.lock`; PDF
OCR is explicitly disabled. The image prefetches the CPU layout/table
artifacts during build, writes a SHA-256 manifest, and starts only when the
local artifacts are present. It does not download models at request time.
PyMuPDF is used only for bounded PDF page rasterization at `/v1/render`; it
does not perform OCR or send data outside the parser process.
Scanned PDFs are returned with an explicit OCR warning and no fabricated text.

For a source checkout without the image dependencies, the service keeps the
stdlib parser fallback so protocol tests remain runnable. Set
`KNOWLEDGE_PARSER_DOCLING=false` to force that fallback explicitly.
